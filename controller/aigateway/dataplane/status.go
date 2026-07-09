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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureReadyStatus reads the owned Deployment's replica counts, copies them to the
// AIGatewayDataPlane status, and evaluates whether all conditions are True to set
// the Ready condition accordingly. Status is not patched here, the caller
// is responsible for flushing via applyStatus.
func ensureReadyStatus(
	ctx context.Context,
	cl client.Client,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) error {
	deployment := &appsv1.Deployment{}
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: aigwdp.Namespace,
		Name:      aigwdp.Name,
	}, deployment); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Deployment not yet created, not ready.
			apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
				Type:               string(aigatewayv1alpha1.ReadyType),
				Status:             metav1.ConditionFalse,
				Reason:             string(aigatewayv1alpha1.DependenciesNotReadyReason),
				Message:            aigatewayv1alpha1.DependenciesNotReadyMessage,
				ObservedGeneration: aigwdp.Generation,
			})
			return nil
		}
		return err
	}

	aigwdp.Status.Replicas = deployment.Status.Replicas
	aigwdp.Status.ReadyReplicas = deployment.Status.ReadyReplicas

	// Mark Not Ready only when zero pods are serving traffic.
	// During a rolling update some replicas are still ready, so we stay Ready
	// throughout the rollout and only flip to False when there is nothing left to serve.
	if deployment.Status.ReadyReplicas == 0 {
		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.ReadyType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.DependenciesNotReadyReason),
			Message:            aigatewayv1alpha1.DependenciesNotReadyMessage,
			ObservedGeneration: aigwdp.Generation,
		})
	} else {
		k8sutils.SetReadyWithGeneration(aigwdp, aigwdp.Generation)
	}

	return nil
}

// applyStatus patches the AIGatewayDataPlane status subresource via SSA.
func (r *Reconciler) applyStatus(
	ctx context.Context,
	logger logr.Logger,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) error {
	result, statusErr := controllerpkgssa.ApplyStatusIfChanged(ctx, logger, r.Client, r.TypeConverter, aigwdp, controllerpkgssa.FieldManager)
	if statusErr != nil {
		log.Error(logger, statusErr, "failed to patch AIGatewayDataPlane status")
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeWarning, "StatusPatchFailed", "PatchStatus", "%s", statusErr.Error())
		return statusErr
	}
	if result == op.Updated {
		log.Debug(logger, "AIGatewayDataPlane status updated")
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeNormal, "StatusUpdated", "PatchStatus", "AIGatewayDataPlane status updated")
	}
	return nil
}
