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
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
)

// resolveOnPremAIGateway resolves the OnPremAIGateway referenced by the
// AIGatewayDataPlane via spec.controlPlaneRef.onpremNamespacedRef. It sets the
// OnPremAIGatewayResolved condition on the AIGatewayDataPlane and returns the
// resolved OnPremAIGateway if it exists and is Ready.
func (r *Reconciler) resolveOnPremAIGateway(
	ctx context.Context,
	logger logr.Logger,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) (*aigatewayv1alpha1.OnPremAIGateway, error) {
	if aigwdp.Spec.ControlPlaneRef == nil || aigwdp.Spec.ControlPlaneRef.OnPremNamespacedRef == nil {
		return nil, nil
	}

	onprem := &aigatewayv1alpha1.OnPremAIGateway{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      aigwdp.Spec.ControlPlaneRef.OnPremNamespacedRef.Name,
		Namespace: aigwdp.Namespace,
	}, onprem)

	if apierrors.IsNotFound(err) {
		log.Debug(logger, "referenced OnPremAIGateway not found",
			"ref", aigwdp.Spec.ControlPlaneRef.OnPremNamespacedRef.Name)

		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.OnPremAIGatewayResolvedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
			Message:            aigatewayv1alpha1.OnPremAIGatewayNotFoundMessage,
			ObservedGeneration: aigwdp.Generation,
		})

		return nil, err
	}
	if err != nil {
		return nil, err
	}

	// Check that the OnPremAIGateway is Ready (i.e. its control plane instance
	// is up and able to push configuration to the data planes referencing it).
	if !apimeta.IsStatusConditionTrue(onprem.Status.Conditions, string(aigatewayv1alpha1.ReadyType)) {
		log.Debug(logger, "referenced OnPremAIGateway is not yet Ready",
			"ref", aigwdp.Spec.ControlPlaneRef.OnPremNamespacedRef.Name)

		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.OnPremAIGatewayResolvedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.OnPremAIGatewayNotReadyReason),
			Message:            aigatewayv1alpha1.OnPremAIGatewayNotReadyMessage,
			ObservedGeneration: aigwdp.Generation,
		})

		return nil, fmt.Errorf("referenced OnPremAIGateway %q is not yet Ready",
			aigwdp.Spec.ControlPlaneRef.OnPremNamespacedRef.Name)
	}

	apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
		Type:               string(aigatewayv1alpha1.OnPremAIGatewayResolvedType),
		Status:             metav1.ConditionTrue,
		Reason:             string(aigatewayv1alpha1.ControlPlaneResolvedReason),
		Message:            aigatewayv1alpha1.OnPremAIGatewayResolvedMessage,
		ObservedGeneration: aigwdp.Generation,
	})

	return onprem, nil
}
