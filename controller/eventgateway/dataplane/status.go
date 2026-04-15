/*
Copyright 2025 Kong, Inc.

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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureReadyStatus reads the owned Deployment's replica counts, copies them to the
// DataPlane status, and evaluates whether all conditions are True to set
// the Ready condition accordingly. Status is not patched here, the caller
// is responsible for flushing via patchStatus.
func ensureReadyStatus(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
) error {
	deployment := &appsv1.Deployment{}
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: egdp.Namespace,
		Name:      egdp.Name,
	}, deployment); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Deployment not yet created, not ready.
			apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
				Type:               string(eventgatewayv1alpha1.ReadyType),
				Status:             metav1.ConditionFalse,
				Reason:             string(eventgatewayv1alpha1.DependenciesNotReadyReason),
				Message:            eventgatewayv1alpha1.DependenciesNotReadyMessage,
				ObservedGeneration: egdp.Generation,
			})
			return nil
		}
		return err
	}

	egdp.Status.Replicas = deployment.Status.Replicas
	egdp.Status.ReadyReplicas = deployment.Status.ReadyReplicas

	// Mark Not Ready only when zero pods are serving traffic.
	// During a rolling update some replicas are still ready, so we stay Ready
	// throughout the rollout and only flip to False when there is nothing left to serve.
	if deployment.Status.ReadyReplicas == 0 {
		apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
			Type:               string(eventgatewayv1alpha1.ReadyType),
			Status:             metav1.ConditionFalse,
			Reason:             string(eventgatewayv1alpha1.DependenciesNotReadyReason),
			Message:            eventgatewayv1alpha1.DependenciesNotReadyMessage,
			ObservedGeneration: egdp.Generation,
		})
	} else {
		k8sutils.SetReadyWithGeneration(egdp, egdp.Generation)
	}

	return nil
}

// applyStatus patches the KegDataPlane status subresource via SSA and joins
// any error into *err so the caller's named return reflects the failure.
func (r *Reconciler) applyStatus(
	ctx context.Context,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	err *error,
) {
	result, statusErr := controllerpkgssa.ApplyStatusIfChanged(ctx, logger, r.Client, r.typeConverter, egdp, FieldManager)
	if statusErr != nil {
		log.Error(logger, statusErr, "failed to patch KegDataPlane status")
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeWarning, "StatusPatchFailed", "PatchStatus", "%s", statusErr.Error())
		*err = errors.Join(*err, statusErr)
		return
	}
	if result == op.Updated {
		log.Debug(logger, "KegDataPlane status updated")
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeNormal, "StatusUpdated", "PatchStatus", "KegDataPlane status updated")
	}
}
