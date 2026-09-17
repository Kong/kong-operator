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

// errControlPlaneNotReady is returned by resolveControlPlane when the
// referenced non-Konnect control plane exists but is not yet Ready. Like
// errControlPlaneNotProgrammed it is an expected transient state.
var errControlPlaneNotReady = errors.New("control plane is not yet Ready")

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

	cp := cpKindCfg.NewObject()
	err := r.Get(ctx, types.NamespacedName{
		Name:      ref.Name,
		Namespace: dp.GetNamespace(),
	}, cp)

	if apierrors.IsNotFound(err) {
		log.Debug(logger, "referenced "+cpKindCfg.Kind+" not found",
			"ref", ref.Name)

		setStatusCondition(dp, metav1.Condition{
			Type:               cpKindCfg.Conditions.ResolvedType,
			Status:             metav1.ConditionFalse,
			Reason:             cpKindCfg.Conditions.NotFoundReason,
			Message:            cpKindCfg.Conditions.NotFoundMessage,
			ObservedGeneration: dp.GetGeneration(),
		})

		return ResolvedControlPlane{}, err
	}
	if err != nil {
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

		setStatusCondition(dp, metav1.Condition{
			Type:               cpKindCfg.Conditions.ResolvedType,
			Status:             metav1.ConditionFalse,
			Reason:             cpKindCfg.Conditions.NotProgrammedReason,
			Message:            cpKindCfg.Conditions.NotProgrammedMessage,
			ObservedGeneration: dp.GetGeneration(),
		})

		return resolved, fmt.Errorf("referenced %s %q: %w",
			cpKindCfg.Kind, ref.Name, errControlPlaneNotProgrammed)
	}
	if !cpKindCfg.IsKonnect && cpKindCfg.ReadinessConditionType != "" &&
		!apimeta.IsStatusConditionTrue(cp.GetConditions(), cpKindCfg.ReadinessConditionType) {
		log.Debug(logger, "referenced "+cpKindCfg.Kind+" is not yet Ready",
			"ref", ref.Name)

		setStatusCondition(dp, metav1.Condition{
			Type:               cpKindCfg.Conditions.ResolvedType,
			Status:             metav1.ConditionFalse,
			Reason:             cpKindCfg.Conditions.NotReadyReason,
			Message:            cpKindCfg.Conditions.NotReadyMessage,
			ObservedGeneration: dp.GetGeneration(),
		})

		return resolved, fmt.Errorf("referenced %s %q: %w",
			cpKindCfg.Kind, ref.Name, errControlPlaneNotReady)
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
