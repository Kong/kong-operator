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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
)

// resolveControlPlane resolves the control plane referenced by the DataPlane.
// It sets the control plane resolved condition on the DataPlane and returns
// the resolved control plane if successful.
func (r *Reconciler[T, CP, Cert]) resolveControlPlane(
	ctx context.Context,
	logger logr.Logger,
	dp T,
	cpName string,
) (CP, error) {
	cp := r.Config.NewControlPlaneObject()
	err := r.Get(ctx, types.NamespacedName{
		Name:      cpName,
		Namespace: dp.GetNamespace(),
	}, cp)

	if apierrors.IsNotFound(err) {
		log.Debug(logger, "referenced "+r.Config.ControlPlaneKind+" not found",
			"ref", cpName)

		setStatusCondition(dp, metav1.Condition{
			Type:               r.Config.Conditions.ControlPlaneResolvedType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.ControlPlaneNotFoundReason,
			Message:            r.Config.Conditions.ControlPlaneNotFoundMessage,
			ObservedGeneration: dp.GetGeneration(),
		})

		return cp, err
	}
	if err != nil {
		return cp, err
	}

	// Check that the control plane is Programmed (i.e. exists in Konnect).
	if !apimeta.IsStatusConditionTrue(cp.GetConditions(), konnectv1alpha1.KonnectEntityProgrammedConditionType) {
		log.Debug(logger, "referenced "+r.Config.ControlPlaneKind+" is not yet Programmed",
			"ref", cpName)

		setStatusCondition(dp, metav1.Condition{
			Type:               r.Config.Conditions.ControlPlaneResolvedType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.ControlPlaneNotProgrammedReason,
			Message:            r.Config.Conditions.ControlPlaneNotProgrammedMessage,
			ObservedGeneration: dp.GetGeneration(),
		})

		return cp, fmt.Errorf("referenced %s %q is not yet Programmed",
			r.Config.ControlPlaneKind, cpName)
	}

	setStatusCondition(dp, metav1.Condition{
		Type:               r.Config.Conditions.ControlPlaneResolvedType,
		Status:             metav1.ConditionTrue,
		Reason:             r.Config.Conditions.ControlPlaneResolvedReason,
		Message:            r.Config.Conditions.ControlPlaneResolvedMessage,
		ObservedGeneration: dp.GetGeneration(),
	})

	return cp, nil
}
