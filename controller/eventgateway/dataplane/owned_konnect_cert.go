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

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureKonnectCertificate ensures a EventGatewayDataPlaneCertificate resource
// exists for the given DataPlane, referencing the provisioned mTLS Secret and the
// resolved KonnectEventGateway.
func (r *Reconciler) ensureKonnectCertificate(
	ctx context.Context,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	keg *konnectv1alpha1.KonnectEventGateway,
	certSecret *corev1.Secret,
) (programmed bool, err error) {
	desired := &configurationv1alpha1.EventGatewayDataPlaneCertificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayDataPlaneCertificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      egdp.Name,
			Namespace: egdp.Namespace,
		},
		Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: keg.Name,
				},
			},
			APISpec: configurationv1alpha1.EventGatewayDataPlaneCertificateAPISpec{
				Certificate: configurationv1alpha1.SensitiveDataSource{
					Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name: certSecret.Name,
					},
				},
			},
		},
	}

	k8sutils.SetOwnerForObject(desired, egdp)

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.typeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
			Type:               string(eventgatewayv1alpha1.KonnectCertificateRegisteredType),
			Status:             metav1.ConditionFalse,
			Reason:             string(eventgatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
			Message:            fmt.Sprintf("failed to ensure EventGatewayDataPlaneCertificate: %v", err),
			ObservedGeneration: egdp.Generation,
		})
		return false, fmt.Errorf("failed to apply EventGatewayDataPlaneCertificate for DataPlane %s/%s: %w",
			egdp.Namespace, egdp.Name, err)
	}

	switch result {
	case op.Created:
		log.Debug(logger, "EventGatewayDataPlaneCertificate created", "name", desired.Name)
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeNormal, "KonnectCertificateCreated", "CreateKonnectCertificate",
			"EventGatewayDataPlaneCertificate %s created", desired.Name)
	case op.Updated:
		log.Debug(logger, "EventGatewayDataPlaneCertificate updated", "name", desired.Name)
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeNormal, "KonnectCertificateUpdated", "UpdateKonnectCertificate",
			"EventGatewayDataPlaneCertificate %s updated", desired.Name)
	case op.Noop, op.Deleted:
	}

	programmed, err = checkKonnectCertificateProgrammed(ctx, r.Client, egdp, desired)
	if err != nil {
		apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
			Type:               string(eventgatewayv1alpha1.KonnectCertificateRegisteredType),
			Status:             metav1.ConditionFalse,
			Reason:             string(eventgatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
			Message:            fmt.Sprintf("failed to check EventGatewayDataPlaneCertificate status: %v", err),
			ObservedGeneration: egdp.Generation,
		})
		return false, err
	}
	if !programmed {
		return false, nil
	}
	apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
		Type:               string(eventgatewayv1alpha1.KonnectCertificateRegisteredType),
		Status:             metav1.ConditionTrue,
		Reason:             string(eventgatewayv1alpha1.KonnectCertificateRegisteredReason),
		Message:            "EventGatewayDataPlaneCertificate ensured and programmed on Konnect",
		ObservedGeneration: egdp.Generation,
	})
	return true, nil
}

// checkKonnectCertificateProgrammed fetches the EventGatewayDataPlaneCertificate
// and checks whether the Konnect controller has programmed it on the Konnect API.
// It sets KonnectCertificateRegistered=False on egdp when not yet programmed and
// returns false so the caller can return early; the Owns() watch will retrigger
// once the Konnect controller flips Programmed to True.
func checkKonnectCertificateProgrammed(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	desired *configurationv1alpha1.EventGatewayDataPlaneCertificate,
) (bool, error) {
	current := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		return false, fmt.Errorf("failed to get EventGatewayDataPlaneCertificate %s/%s: %w",
			desired.Namespace, desired.Name, err)
	}

	programmedCond := apimeta.FindStatusCondition(current.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType)
	if programmedCond == nil || programmedCond.Status != metav1.ConditionTrue {
		// Not yet programmed, update condition and return early. The Owns()
		// watch on EventGatewayDataPlaneCertificate will retrigger once the
		// Konnect controller flips Programmed to True.
		apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
			Type:               string(eventgatewayv1alpha1.KonnectCertificateRegisteredType),
			Status:             metav1.ConditionFalse,
			Reason:             string(eventgatewayv1alpha1.KonnectCertificateNotProgrammedReason),
			Message:            "EventGatewayDataPlaneCertificate is not yet programmed on Konnect",
			ObservedGeneration: egdp.Generation,
		})
		return false, nil
	}
	return true, nil
}
