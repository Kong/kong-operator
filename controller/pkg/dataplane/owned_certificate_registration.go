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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureCertificateRegistration ensures a DataPlane certificate resource exists
// for the given DataPlane, referencing the provisioned mTLS Secret and the
// resolved control plane (Konnect, or a non-Konnect kind with
// CertificateRegistration), which its controller registers the certificate
// on.
func (r *Reconciler[T, Cert]) ensureCertificateRegistration(
	ctx context.Context,
	logger logr.Logger,
	dp T,
	cp ResolvedControlPlane,
	certSecret *corev1.Secret,
	certChecksum string,
) (programmed bool, err error) {
	conds := r.certificateRegistrationConditions(cp)
	desired := r.Config.Certificate.Build(dp, cp, certSecret.Name, certChecksum)

	k8sutils.SetOwnerForObject(desired, dp)

	if adapt := r.Config.Certificate.AdaptToExisting; adapt != nil {
		existing := r.Config.NewCertificateObject()
		err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
		switch {
		case err == nil:
			adapt(desired, existing)
		case !apierrors.IsNotFound(err):
			return false, fmt.Errorf("failed to get %s %s/%s: %w",
				r.Config.Certificate.Kind, desired.GetNamespace(), desired.GetName(), err)
		}
	}

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		setStatusCondition(dp, metav1.Condition{
			Type:               conds.Type,
			Status:             metav1.ConditionFalse,
			Reason:             conds.RegistrationFailedReason,
			Message:            fmt.Sprintf("failed to ensure %s: %v", r.Config.Certificate.Kind, err),
			ObservedGeneration: dp.GetGeneration(),
		})
		return false, fmt.Errorf("failed to apply %s for %s %s/%s: %w",
			r.Config.Certificate.Kind, r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}

	switch result {
	case op.Created:
		log.Debug(logger, r.Config.Certificate.Kind+" created", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, conds.CreatedEventReason, conds.CreatedEventAction,
			"%s %s created", r.Config.Certificate.Kind, desired.GetName())
	case op.Updated:
		log.Debug(logger, r.Config.Certificate.Kind+" updated", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, conds.UpdatedEventReason, conds.UpdatedEventAction,
			"%s %s updated", r.Config.Certificate.Kind, desired.GetName())
	case op.Noop, op.Deleted:
	}

	programmed, err = r.checkCertificateProgrammed(ctx, dp, desired, conds)
	if err != nil {
		setStatusCondition(dp, metav1.Condition{
			Type:               conds.Type,
			Status:             metav1.ConditionFalse,
			Reason:             conds.RegistrationFailedReason,
			Message:            fmt.Sprintf("failed to check %s status: %v", r.Config.Certificate.Kind, err),
			ObservedGeneration: dp.GetGeneration(),
		})
		return false, err
	}
	if !programmed {
		return false, nil
	}
	setStatusCondition(dp, metav1.Condition{
		Type:               conds.Type,
		Status:             metav1.ConditionTrue,
		Reason:             conds.RegisteredReason,
		Message:            r.Config.Certificate.Kind + " ensured and programmed on " + conds.Target,
		ObservedGeneration: dp.GetGeneration(),
	})
	return true, nil
}

// certificateRegistrationConditions returns the conditions the registration
// of the client certificate on the control plane is reported with: those of
// its kind, or the Konnect ones.
func (r *Reconciler[T, Cert]) certificateRegistrationConditions(cp ResolvedControlPlane) CertificateRegistrationConditions {
	if cp.CertificateRegistration != nil {
		return *cp.CertificateRegistration
	}
	return CertificateRegistrationConditions{
		Type:                     r.Config.Conditions.KonnectCertificateRegisteredType,
		RegisteredReason:         r.Config.Conditions.KonnectCertificateRegisteredReason,
		RegistrationFailedReason: r.Config.Conditions.KonnectCertificateRegistrationFailedReason,
		NotProgrammedReason:      r.Config.Conditions.KonnectCertificateNotProgrammedReason,
		Target:                   "Konnect",
		CreatedEventReason:       "KonnectCertificateCreated",
		CreatedEventAction:       "CreateKonnectCertificate",
		UpdatedEventReason:       "KonnectCertificateUpdated",
		UpdatedEventAction:       "UpdateKonnectCertificate",
	}
}

// checkCertificateProgrammed fetches the DataPlane certificate and checks
// whether its controller has programmed it on the control plane (the Konnect
// API, or gw-admin) at its current generation: a Programmed condition of an
// earlier generation tells about a previous spec, e.g. another Secret. It
// sets the registration condition to False on the DataPlane when not yet
// programmed and returns false so the caller can return early; the Owns()
// watch will retrigger once the controller of the certificate flips
// Programmed to True.
func (r *Reconciler[T, Cert]) checkCertificateProgrammed(
	ctx context.Context,
	dp T,
	desired Cert,
	conds CertificateRegistrationConditions,
) (bool, error) {
	markNotProgrammed := func(message string) (bool, error) {
		setStatusCondition(dp, metav1.Condition{
			Type:               conds.Type,
			Status:             metav1.ConditionFalse,
			Reason:             conds.NotProgrammedReason,
			Message:            message,
			ObservedGeneration: dp.GetGeneration(),
		})
		return false, nil
	}

	current := r.Config.NewCertificateObject()
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("failed to get %s %s/%s: %w",
				r.Config.Certificate.Kind, desired.GetNamespace(), desired.GetName(), err)
		}
		// A NotFound here means the informer cache has not caught up with the
		// apply yet; treat it as not-programmed. The Owns() watch on the
		// certificate will retrigger once the controller of the certificate
		// flips Programmed to True.
		return markNotProgrammed(r.notProgrammedMessage(conds, nil, 0))
	}

	// A certificate being deleted is going away, whatever its status still says:
	// a certificate named after its content can be needed again meanwhile, e.g.
	// once the previous one is back in a Manual Secret. The Owns() watch retriggers
	// once it is gone, and it is created again.
	if !current.GetDeletionTimestamp().IsZero() {
		return markNotProgrammed(r.Config.Certificate.Kind + " is being deleted: it is created again once it is gone")
	}

	programmedCond := apimeta.FindStatusCondition(current.GetConditions(), konnectv1alpha1.KonnectEntityProgrammedConditionType)
	if programmedCond == nil || programmedCond.Status != metav1.ConditionTrue ||
		programmedCond.ObservedGeneration != current.GetGeneration() {
		return markNotProgrammed(r.notProgrammedMessage(conds, programmedCond, current.GetGeneration()))
	}
	return true, nil
}

// notProgrammedMessage returns the message of the registration condition
// while the certificate object isn't Programmed: with the reason and message
// of its Programmed condition (programmedCond) at its current generation when
// the kind reports it (ReportProgrammedCondition), or with the
// NotProgrammedHint of the kind while the certificate object has none, e.g.
// when nothing programs it.
func (r *Reconciler[T, Cert]) notProgrammedMessage(conds CertificateRegistrationConditions, programmedCond *metav1.Condition, generation int64) string {
	msg := r.Config.Certificate.Kind + " is not yet programmed on " + conds.Target
	switch {
	case programmedCond == nil:
		return msg + conds.NotProgrammedHint
	case !conds.ReportProgrammedCondition || programmedCond.Status == metav1.ConditionTrue ||
		programmedCond.ObservedGeneration != generation:
		// A Programmed condition of an earlier generation tells about a
		// previous spec.
		return msg
	case programmedCond.Message != "":
		return msg + ": " + programmedCond.Reason + ": " + programmedCond.Message
	default:
		return msg + ": " + programmedCond.Reason
	}
}
