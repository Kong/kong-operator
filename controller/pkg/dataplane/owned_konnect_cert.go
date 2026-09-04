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

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureKonnectCertificate ensures a DataPlane certificate resource exists for
// the given DataPlane, referencing the provisioned mTLS Secret and the
// resolved control plane.
func (r *Reconciler[T, CP, Cert]) ensureKonnectCertificate(
	ctx context.Context,
	logger logr.Logger,
	dp T,
	cp CP,
	certSecret *corev1.Secret,
) (programmed bool, err error) {
	desired := r.Config.BuildCertificate(dp, cp, certSecret.Name)

	k8sutils.SetOwnerForObject(desired, dp)

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		setStatusCondition(dp, metav1.Condition{
			Type:               r.Config.Conditions.KonnectCertificateRegisteredType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.KonnectCertificateRegistrationFailedReason,
			Message:            fmt.Sprintf("failed to ensure %s: %v", r.Config.CertificateKind, err),
			ObservedGeneration: dp.GetGeneration(),
		})
		return false, fmt.Errorf("failed to apply %s for %s %s/%s: %w",
			r.Config.CertificateKind, r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}

	switch result {
	case op.Created:
		log.Debug(logger, r.Config.CertificateKind+" created", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "KonnectCertificateCreated", "CreateKonnectCertificate",
			"%s %s created", r.Config.CertificateKind, desired.GetName())
	case op.Updated:
		log.Debug(logger, r.Config.CertificateKind+" updated", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "KonnectCertificateUpdated", "UpdateKonnectCertificate",
			"%s %s updated", r.Config.CertificateKind, desired.GetName())
	case op.Noop, op.Deleted:
	}

	programmed, err = r.checkKonnectCertificateProgrammed(ctx, dp, desired)
	if err != nil {
		setStatusCondition(dp, metav1.Condition{
			Type:               r.Config.Conditions.KonnectCertificateRegisteredType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.KonnectCertificateRegistrationFailedReason,
			Message:            fmt.Sprintf("failed to check %s status: %v", r.Config.CertificateKind, err),
			ObservedGeneration: dp.GetGeneration(),
		})
		return false, err
	}
	if !programmed {
		return false, nil
	}
	setStatusCondition(dp, metav1.Condition{
		Type:               r.Config.Conditions.KonnectCertificateRegisteredType,
		Status:             metav1.ConditionTrue,
		Reason:             r.Config.Conditions.KonnectCertificateRegisteredReason,
		Message:            r.Config.CertificateKind + " ensured and programmed on Konnect",
		ObservedGeneration: dp.GetGeneration(),
	})
	return true, nil
}

// checkKonnectCertificateProgrammed fetches the DataPlane certificate and
// checks whether the Konnect controller has programmed it on the Konnect API.
// It sets KonnectCertificateRegistered=False on the DataPlane when not yet
// programmed and returns false so the caller can return early; the Owns()
// watch will retrigger once the Konnect controller flips Programmed to True.
func (r *Reconciler[T, CP, Cert]) checkKonnectCertificateProgrammed(
	ctx context.Context,
	dp T,
	desired Cert,
) (bool, error) {
	current := r.Config.NewCertificateObject()
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		return false, fmt.Errorf("failed to get %s %s/%s: %w",
			r.Config.CertificateKind, desired.GetNamespace(), desired.GetName(), err)
	}

	programmedCond := apimeta.FindStatusCondition(current.GetConditions(), konnectv1alpha1.KonnectEntityProgrammedConditionType)
	if programmedCond == nil || programmedCond.Status != metav1.ConditionTrue {
		// Not yet programmed, update condition and return early. The Owns()
		// watch on the certificate will retrigger once the Konnect controller
		// flips Programmed to True.
		setStatusCondition(dp, metav1.Condition{
			Type:               r.Config.Conditions.KonnectCertificateRegisteredType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.KonnectCertificateNotProgrammedReason,
			Message:            r.Config.CertificateKind + " is not yet programmed on Konnect",
			ObservedGeneration: dp.GetGeneration(),
		})
		return false, nil
	}
	return true, nil
}
