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

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// setStatusCondition sets the condition on the DataPlane, preserving the
// apimeta.SetStatusCondition semantics (LastTransitionTime is only bumped when
// the status changes).
func setStatusCondition(dp k8sutils.ConditionsAware, condition metav1.Condition) {
	conditions := dp.GetConditions()
	apimeta.SetStatusCondition(&conditions, condition)
	dp.SetConditions(conditions)
}

// ensureReadyStatus computes the Ready condition for a DataPlane.
// It first checks whether any non-Ready condition is False; if so it sets
// Ready=False immediately without fetching the Deployment. Otherwise it reads
// the owned Deployment and sets Ready based on DeploymentRolloutComplete,
// which requires the controller to have observed the current generation and
// all desired replicas to be updated and available.
// Status is not patched here; the caller flushes via applyStatus.
func (r *Reconciler[T, CP, Cert]) ensureReadyStatus(
	ctx context.Context,
	dp T,
) error {
	for _, c := range dp.GetConditions() {
		if c.Type != r.Config.Conditions.ReadyType && c.Status == metav1.ConditionFalse {
			setStatusCondition(dp, metav1.Condition{
				Type:               r.Config.Conditions.ReadyType,
				Status:             metav1.ConditionFalse,
				Reason:             r.Config.Conditions.DependenciesNotReadyReason,
				Message:            c.Message,
				ObservedGeneration: dp.GetGeneration(),
			})
			return nil
		}
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: dp.GetNamespace(),
		Name:      dp.GetName(),
	}, deployment); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Deployment not yet created, not ready.
			setStatusCondition(dp, metav1.Condition{
				Type:               r.Config.Conditions.ReadyType,
				Status:             metav1.ConditionFalse,
				Reason:             r.Config.Conditions.DependenciesNotReadyReason,
				Message:            r.Config.Conditions.DependenciesNotReadyMessage,
				ObservedGeneration: dp.GetGeneration(),
			})
			return nil
		}
		return err
	}

	r.Config.SetStatusReplicas(dp, deployment.Status.Replicas, deployment.Status.ReadyReplicas)

	// Ready only once the current generation has been fully rolled out, not
	// merely once some pods happen to be ready.
	if !k8sutils.DeploymentRolloutComplete(deployment) {
		setStatusCondition(dp, metav1.Condition{
			Type:               r.Config.Conditions.ReadyType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.WaitingToBecomeReadyReason,
			Message:            r.Config.Conditions.WaitingToBecomeReadyMessage,
			ObservedGeneration: dp.GetGeneration(),
		})
	} else {
		k8sutils.SetReadyWithGeneration(dp, dp.GetGeneration())
	}

	return nil
}

// applyStatus patches the DataPlane status subresource via SSA.
func (r *Reconciler[T, CP, Cert]) applyStatus(
	ctx context.Context,
	logger logr.Logger,
	dp T,
) error {
	result, statusErr := controllerpkgssa.ApplyStatusIfChanged(ctx, logger, r.Client, r.TypeConverter, dp, controllerpkgssa.FieldManager)
	if statusErr != nil {
		log.Error(logger, statusErr, "failed to patch "+r.Config.Kind+" status")
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeWarning, "StatusPatchFailed", "PatchStatus", "%s", statusErr.Error())
		return statusErr
	}
	if result == op.Updated {
		log.Debug(logger, r.Config.Kind+" status updated")
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "StatusUpdated", "PatchStatus", "%s status updated", r.Config.Kind)
	}
	return nil
}
