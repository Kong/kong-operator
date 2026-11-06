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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
)

// errControlPlaneNotProgrammed is returned by resolveControlPlane when the
// referenced control plane exists but is not yet Programmed on Konnect. It is
// an expected transient state: the resolution condition is set and the control
// plane watch re-triggers the reconcile, so callers should not retry it with
// error backoff.
var errControlPlaneNotProgrammed = errors.New("control plane is not yet Programmed")

// errControlPlaneKindDisabled is returned by resolveControlPlane when the
// kind of the referenced control plane is disabled (see
// ControlPlaneKindConfig.DisabledMessage).
var errControlPlaneKindDisabled = errors.New("control plane kind is disabled")

// errControlPlaneNotReady is returned by resolveControlPlane when the
// referenced non-Konnect control plane exists but is not yet Ready. Like
// errControlPlaneNotProgrammed it is an expected transient state.
var errControlPlaneNotReady = errors.New("control plane is not yet Ready")

// errEndpointsMismatch is returned by resolveControlPlane when the referenced
// control plane is Programmed but returns endpoints its serverURL cannot be
// used with. Like errControlPlaneNotProgrammed it is an expected transient
// state, even though only a user or Konnect can resolve it.
var errEndpointsMismatch = errors.New("control plane endpoints cannot be used with its serverURL")

// errControlPlaneGone is wrapped by the error resolveControlPlane returns
// when the referenced control plane won't be resolved again: it doesn't
// exist, or its dependencies tell it is gone (see
// DependenciesNotReadyError.ControlPlaneGone).
var errControlPlaneGone = errors.New("control plane is gone")

// resolveControlPlane resolves the control plane referenced by the DataPlane.
// It sets the kind-specific control plane resolved condition on the DataPlane
// and returns the resolved control plane if successful.
//
// Resolution also removes the resolution conditions of all other configured
// control plane kinds: should a DataPlane kind with a mutable control plane
// reference ever switch kinds (AIGatewayDataPlane's is immutable), the
// previous kind's stale resolution condition would otherwise never be cleared
// from status, since each kind reports under its own condition type and SSA
// keeps untouched entries.
func (r *Reconciler[T, Cert]) resolveControlPlane(
	ctx context.Context,
	logger logr.Logger,
	dp T,
	ref ControlPlaneRef,
) (ResolvedControlPlane, error) {
	cpKindCfg, ok := r.Config.ControlPlaneKindConfig(ref.Kind)
	if !ok {
		return ResolvedControlPlane{}, fmt.Errorf(
			"%s %s/%s references unsupported control plane kind %q in controlPlaneRef",
			r.Config.Kind, dp.GetNamespace(), dp.GetName(), ref.Kind)
	}

	// Drop the resolution conditions of all other configured control plane
	// kinds: only the kind currently referenced may report on resolution.
	for _, other := range r.Config.ControlPlanes {
		if other.Kind == cpKindCfg.Kind {
			continue
		}
		removeStatusCondition(dp, other.Conditions.ResolvedType)
	}

	if cpKindCfg.DisabledMessage != "" {
		log.Debug(logger, "referenced "+cpKindCfg.Kind+" kind is disabled",
			"ref", ref.Name)

		r.markUnresolved(dp, cpKindCfg, cpKindCfg.Conditions.NotFoundReason, cpKindCfg.DisabledMessage)

		return ResolvedControlPlane{}, fmt.Errorf("referenced %s %q: %w",
			cpKindCfg.Kind, ref.Name, errControlPlaneKindDisabled)
	}

	cp := cpKindCfg.NewObject()
	err := r.Get(ctx, types.NamespacedName{
		Name:      ref.Name,
		Namespace: dp.GetNamespace(),
	}, cp)

	if apierrors.IsNotFound(err) {
		log.Debug(logger, "referenced "+cpKindCfg.Kind+" not found",
			"ref", ref.Name)

		r.markUnresolved(dp, cpKindCfg, cpKindCfg.Conditions.NotFoundReason, cpKindCfg.Conditions.NotFoundMessage)

		return ResolvedControlPlane{}, fmt.Errorf("%w: %w", errControlPlaneGone, err)
	}
	if err != nil {
		// Conditions left True would say the certificate is still valid and
		// registered, and Ready would say so too. Konnect-backed kinds keep
		// their conditions (see markCertificateUnknown).
		if cpKindCfg.CertificateRegistration != nil {
			r.markUnresolved(dp, cpKindCfg, cpKindCfg.Conditions.NotReadyReason, err.Error())
		}
		return ResolvedControlPlane{}, err
	}

	resolved := ResolvedControlPlane{
		Kind:      cpKindCfg.Kind,
		IsKonnect: cpKindCfg.IsKonnect,
		Object:    cp,
	}

	// Konnect-backed control planes must be Programmed (i.e. exist on Konnect);
	// non-Konnect ones must satisfy their configured readiness condition.
	if cpKindCfg.IsKonnect &&
		!apimeta.IsStatusConditionTrue(cp.GetConditions(), konnectv1alpha1.KonnectEntityProgrammedConditionType) {
		log.Debug(logger, "referenced "+cpKindCfg.Kind+" is not yet Programmed",
			"ref", ref.Name)

		r.markUnresolved(dp, cpKindCfg, cpKindCfg.Conditions.NotProgrammedReason, cpKindCfg.Conditions.NotProgrammedMessage)

		return resolved, fmt.Errorf("referenced %s %q: %w",
			cpKindCfg.Kind, ref.Name, errControlPlaneNotProgrammed)
	}

	// A Programmed control plane can still hand out endpoints the DataPlane
	// cannot use: holding it here keeps a data plane from booting against the
	// wrong Konnect endpoint.
	if cpKindCfg.EndpointCheck != nil {
		if err := cpKindCfg.EndpointCheck(cp); err != nil {
			log.Debug(logger, "referenced "+cpKindCfg.Kind+" has unusable endpoints",
				"ref", ref.Name, "error", err.Error())

			setStatusCondition(dp, metav1.Condition{
				Type:               cpKindCfg.Conditions.ResolvedType,
				Status:             metav1.ConditionFalse,
				Reason:             cpKindCfg.Conditions.EndpointMismatchReason,
				Message:            err.Error(),
				ObservedGeneration: dp.GetGeneration(),
			})

			return resolved, fmt.Errorf("referenced %s %q: %w",
				cpKindCfg.Kind, ref.Name, errEndpointsMismatch)
		}
	}

	if !cpKindCfg.IsKonnect && cpKindCfg.ReadinessConditionType != "" &&
		!apimeta.IsStatusConditionTrue(cp.GetConditions(), cpKindCfg.ReadinessConditionType) {
		log.Debug(logger, "referenced "+cpKindCfg.Kind+" is not yet Ready",
			"ref", ref.Name)

		r.markUnresolved(dp, cpKindCfg, cpKindCfg.Conditions.NotReadyReason, cpKindCfg.Conditions.NotReadyMessage)

		return resolved, fmt.Errorf("referenced %s %q: %w",
			cpKindCfg.Kind, ref.Name, errControlPlaneNotReady)
	}

	resolved.CertificateRegistration = cpKindCfg.CertificateRegistration
	if cpKindCfg.Dependencies != nil {
		deps, err := cpKindCfg.Dependencies(ctx, r.Client, cp)
		if notReady, ok := errors.AsType[*DependenciesNotReadyError](err); ok {
			log.Debug(logger, "dependencies of the referenced "+cpKindCfg.Kind+" are not ready",
				"ref", ref.Name, "reason", notReady.Message)

			r.markUnresolved(dp, cpKindCfg, cpKindCfg.Conditions.NotReadyReason, notReady.Message)

			if notReady.ControlPlaneGone {
				return resolved, fmt.Errorf("referenced %s %q: %w: %w: %w",
					cpKindCfg.Kind, ref.Name, errControlPlaneNotReady, errControlPlaneGone, err)
			}
			return resolved, fmt.Errorf("referenced %s %q: %w: %w",
				cpKindCfg.Kind, ref.Name, errControlPlaneNotReady, err)
		}
		if err != nil {
			err = fmt.Errorf("failed to read the dependencies of the referenced %s %q: %w",
				cpKindCfg.Kind, ref.Name, err)
			// What the certificate is issued from can't be read: conditions
			// left True would say it is still valid and registered, and Ready
			// would say so too.
			r.markUnresolved(dp, cpKindCfg, cpKindCfg.Conditions.NotReadyReason, err.Error())
			return resolved, err
		}
		resolved.Dependencies = deps
	}

	setStatusCondition(dp, metav1.Condition{
		Type:               cpKindCfg.Conditions.ResolvedType,
		Status:             metav1.ConditionTrue,
		Reason:             cpKindCfg.Conditions.ResolvedReason,
		Message:            cpKindCfg.Conditions.ResolvedMessage,
		ObservedGeneration: dp.GetGeneration(),
	})

	return resolved, nil
}

// markUnresolved sets the control plane resolution condition of the DataPlane
// to False with reason and message, and its certificate conditions to
// Unknown (see markCertificateUnknown).
func (r *Reconciler[T, Cert]) markUnresolved(dp T, cpKindCfg ControlPlaneKindConfig, reason, message string) {
	setStatusCondition(dp, metav1.Condition{
		Type:               cpKindCfg.Conditions.ResolvedType,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dp.GetGeneration(),
	})
	r.markCertificateUnknown(dp, cpKindCfg, reason, message)
}

// markCertificateUnknown sets the certificate conditions of a DataPlane of a
// kind registering its client certificate on the control plane
// (CertificateRegistration) to Unknown when what the certificate is issued
// from and registered on can't be read: whether the certificate is still
// valid and registered can't be told, and conditions left True would say it
// is. Konnect-backed kinds read nothing besides the control plane object, so
// they keep their conditions.
func (r *Reconciler[T, Cert]) markCertificateUnknown(dp T, cpKindCfg ControlPlaneKindConfig, reason, message string) {
	reg := cpKindCfg.CertificateRegistration
	if reg == nil {
		return
	}
	for _, condType := range []string{reg.Type, r.Config.Conditions.CertificateProvisionedType} {
		setStatusCondition(dp, metav1.Condition{
			Type:               condType,
			Status:             metav1.ConditionUnknown,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: dp.GetGeneration(),
		})
	}
}
