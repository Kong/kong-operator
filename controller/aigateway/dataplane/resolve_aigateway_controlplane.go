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

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
)

// resolveAIGatewayControlPlane resolves the AIGatewayControlPlane referenced by the
// AIGatewayDataPlane. It sets the AIGatewayControlPlaneResolved condition on the
// AIGatewayDataPlane and returns the resolved AIGatewayControlPlane if successful.
func (r *Reconciler) resolveAIGatewayControlPlane(
	ctx context.Context,
	logger logr.Logger,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) (*konnectv1alpha1.AIGatewayControlPlane, error) {
	aigwcp := &konnectv1alpha1.AIGatewayControlPlane{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
		Namespace: aigwdp.Namespace,
	}, aigwcp)

	if apierrors.IsNotFound(err) {
		log.Debug(logger, "referenced AIGatewayControlPlane not found",
			"ref", aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)

		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.AIGatewayControlPlaneResolvedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.AIGatewayControlPlaneNotFoundReason),
			Message:            aigatewayv1alpha1.AIGatewayControlPlaneNotFoundMessage,
			ObservedGeneration: aigwdp.Generation,
		})

		return nil, err
	}
	if err != nil {
		return nil, err
	}

	// Check that the AIGatewayControlPlane is Programmed (i.e. exists in Konnect).
	if !apimeta.IsStatusConditionTrue(aigwcp.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType) {
		log.Debug(logger, "referenced AIGatewayControlPlane is not yet Programmed",
			"ref", aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)

		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.AIGatewayControlPlaneResolvedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.AIGatewayControlPlaneNotProgrammedReason),
			Message:            aigatewayv1alpha1.AIGatewayControlPlaneNotProgrammedMessage,
			ObservedGeneration: aigwdp.Generation,
		})

		return nil, fmt.Errorf("referenced AIGatewayControlPlane %q is not yet Programmed",
			aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)
	}

	apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
		Type:               string(aigatewayv1alpha1.AIGatewayControlPlaneResolvedType),
		Status:             metav1.ConditionTrue,
		Reason:             string(aigatewayv1alpha1.AIGatewayControlPlaneResolvedReason),
		Message:            aigatewayv1alpha1.AIGatewayControlPlaneResolvedMessage,
		ObservedGeneration: aigwdp.Generation,
	})

	return aigwcp, nil
}
