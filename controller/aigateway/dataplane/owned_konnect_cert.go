/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureKonnectCertificate ensures an AIGatewayDataPlaneCertificate resource
// exists for the given AIGatewayDataPlane, referencing the provisioned mTLS Secret and the
// resolved AIGatewayControlPlane.
func (r *Reconciler) ensureKonnectCertificate(
	ctx context.Context,
	logger logr.Logger,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	aigatewaycp *konnectv1alpha1.AIGatewayControlPlane,
	certSecret *corev1.Secret,
) (programmed bool, err error) {
	desired := &configurationv1alpha1.AIGatewayDataPlaneCertificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "AIGatewayDataPlaneCertificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      aigwdp.Name,
			Namespace: aigwdp.Namespace,
		},
		Spec: configurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: aigatewaycp.Name,
				},
			},
			APISpec: configurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
				Cert: configurationv1alpha1.SensitiveDataSource{
					Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{
						Name: certSecret.Name,
						Key:  corev1.TLSCertKey,
					},
				},
				Title: aigwdp.Name,
			},
		},
	}

	k8sutils.SetOwnerForObject(desired, aigwdp)

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.KonnectCertificateRegisteredType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
			Message:            fmt.Sprintf("failed to ensure AIGatewayDataPlaneCertificate: %v", err),
			ObservedGeneration: aigwdp.Generation,
		})
		return false, fmt.Errorf("failed to apply AIGatewayDataPlaneCertificate for AIGatewayDataPlane %s/%s: %w",
			aigwdp.Namespace, aigwdp.Name, err)
	}

	switch result {
	case op.Created:
		log.Debug(logger, "AIGatewayDataPlaneCertificate created", "name", desired.Name)
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeNormal, "KonnectCertificateCreated", "CreateKonnectCertificate",
			"AIGatewayDataPlaneCertificate %s created", desired.Name)
	case op.Updated:
		log.Debug(logger, "AIGatewayDataPlaneCertificate updated", "name", desired.Name)
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeNormal, "KonnectCertificateUpdated", "UpdateKonnectCertificate",
			"AIGatewayDataPlaneCertificate %s updated", desired.Name)
	case op.Noop, op.Deleted:
	}

	programmed, err = checkKonnectCertificateProgrammed(ctx, r.Client, aigwdp, desired)
	if err != nil {
		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.KonnectCertificateRegisteredType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
			Message:            fmt.Sprintf("failed to check AIGatewayDataPlaneCertificate status: %v", err),
			ObservedGeneration: aigwdp.Generation,
		})
		return false, err
	}
	if !programmed {
		return false, nil
	}
	apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
		Type:               string(aigatewayv1alpha1.KonnectCertificateRegisteredType),
		Status:             metav1.ConditionTrue,
		Reason:             string(aigatewayv1alpha1.KonnectCertificateRegisteredReason),
		Message:            "AIGatewayDataPlaneCertificate ensured and programmed on Konnect",
		ObservedGeneration: aigwdp.Generation,
	})
	return true, nil
}

// checkKonnectCertificateProgrammed fetches the AIGatewayDataPlaneCertificate
// and checks whether the Konnect controller has programmed it on the Konnect API.
// It sets KonnectCertificateRegistered=False on aigwdp when not yet programmed and
// returns false so the caller can return early; the Owns() watch will retrigger
// once the Konnect controller flips Programmed to True.
func checkKonnectCertificateProgrammed(
	ctx context.Context,
	cl client.Client,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	desired *configurationv1alpha1.AIGatewayDataPlaneCertificate,
) (bool, error) {
	current := &configurationv1alpha1.AIGatewayDataPlaneCertificate{}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		return false, fmt.Errorf("failed to get AIGatewayDataPlaneCertificate %s/%s: %w",
			desired.Namespace, desired.Name, err)
	}

	programmedCond := apimeta.FindStatusCondition(current.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType)
	if programmedCond == nil || programmedCond.Status != metav1.ConditionTrue {
		// Not yet programmed, update condition and return early. The Owns()
		// watch on AIGatewayDataPlaneCertificate will retrigger once the
		// Konnect controller flips Programmed to True.
		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.KonnectCertificateRegisteredType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.KonnectCertificateNotProgrammedReason),
			Message:            "AIGatewayDataPlaneCertificate is not yet programmed on Konnect",
			ObservedGeneration: aigwdp.Generation,
		})
		return false, nil
	}
	return true, nil
}
