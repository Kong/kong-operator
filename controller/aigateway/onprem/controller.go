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

package onprem

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/managedfields"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/finalizer"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/multiinstanceai"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ControllerName is the name used for logging and event recording.
const ControllerName = "onprem-aigateway"

// Reconciler reconciles an OnPremAIGateway object.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time. It is used for diff-before-apply status patches.
	TypeConverter managedfields.TypeConverter

	// InstancesManager runs the in-process on-prem AI Gateway control plane instances, one per
	// OnPremAIGateway resource.
	InstancesManager *multiinstanceai.Manager
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aigatewayv1alpha1.OnPremAIGateway{}).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile moves the current state of an OnPremAIGateway toward the desired state.
func (r *Reconciler) Reconcile(ctx context.Context, onprem *aigatewayv1alpha1.OnPremAIGateway) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, ControllerName, r.LoggingMode)

	log.Trace(logger, "reconciling OnPremAIGateway resource")

	// The mgrID is used to identify the control plane instance in the multi-instance manager.
	mgrID, err := manager.NewID(string(onprem.GetUID()))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create manager ID: %w", err)
	}

	// The resource is being deleted: tear down the instance it was running.
	if !onprem.DeletionTimestamp.IsZero() {
		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			if _, ok := errors.AsType[instances.InstanceNotFoundError](err); ok {
				log.Debug(logger, "control plane instance not found, skipping cleanup")
			} else {
				return ctrl.Result{}, fmt.Errorf("failed to stop instance: %w", err)
			}
		}

		if controllerutil.RemoveFinalizer(onprem, string(OnPremAIGatewayFinalizerInstanceTeardown)) {
			if err := r.Update(ctx, onprem); err != nil {
				return finalizer.HandlePatchOrUpdateError(err, logger)
			}
		}

		log.Debug(logger, "resource cleanup completed, OnPremAIGateway deleted")
		return ctrl.Result{}, nil
	}

	// Ensure the resource has a finalizer so that the instance gets torn down on delete. Without it the
	// typed reconciler never sees the deletion and the instance would keep running.
	if controllerutil.AddFinalizer(onprem, string(OnPremAIGatewayFinalizerInstanceTeardown)) {
		log.Trace(logger, "setting finalizers")
		if err := r.Update(ctx, onprem); err != nil {
			return finalizer.HandlePatchOrUpdateError(err, logger)
		}
		// Requeue to ensure that we do not miss next reconciliation request in case
		// AddFinalizer calls returned true but the update resulted in a noop.
		return ctrl.Result{Requeue: true, RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	cfg := configFromSpec(onprem)

	log.Trace(logger, "checking readiness of the AI Gateway control plane instance")
	if err := r.InstancesManager.IsInstanceReady(mgrID); err != nil {
		log.Trace(logger, "control plane instance not ready yet", "error", err)

		if _, ok := errors.AsType[instances.InstanceNotFoundError](err); ok {
			log.Debug(logger, "control plane instance not found, creating new instance")
			if err := r.scheduleInstance(logger, mgrID, cfg); err != nil {
				return ctrl.Result{}, err
			}
		}
		return r.initStatusToWaitingToBecomeReady(ctx, logger, onprem)
	}

	log.Trace(logger, "checking if the AI Gateway control plane instance config matches the spec")
	hashRunning, err := r.InstancesManager.GetInstanceConfigHash(mgrID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get instance config hash: %w", err)
	}
	hashFromSpec, err := multiinstanceai.Hash(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to hash OnPremAIGateway config: %w", err)
	}

	// The running instance's config drifted from the spec: restart it with the new config.
	if hashRunning != hashFromSpec {
		log.Debug(logger, "control plane instance config does not match the spec, restarting instance")
		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to stop instance: %w", err)
		}
		if err := r.scheduleInstance(logger, mgrID, cfg); err != nil {
			// The stopped instance is removed from the manager asynchronously, so it can still be
			// registered here. Requeue and reschedule once it's gone.
			if _, ok := errors.AsType[instances.InstanceWithIDAlreadyScheduledError](err); !ok {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "stopped instance not reaped yet, retrying")
		}
		return r.initStatusToWaitingToBecomeReady(ctx, logger, onprem)
	}

	onprem.Status.ConfigHash = hashRunning
	k8sutils.SetReadyWithGeneration(onprem, onprem.Generation)

	if err := r.applyStatus(ctx, logger, onprem); err != nil {
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for OnPremAIGateway resource")
	return ctrl.Result{}, nil
}

// configFromSpec builds the control plane instance configuration from the OnPremAIGateway spec.
//
// The spec is empty for now, so this returns the zero config. It exists as the seam that the
// configuration assembly plugs into.
// TODO: https://github.com/Kong/kong-operator/issues/5569
func configFromSpec(_ *aigatewayv1alpha1.OnPremAIGateway) multiinstanceai.Config {
	return multiinstanceai.Config{}
}

// scheduleInstance creates a new control plane instance and schedules it in the multi-instance manager.
func (r *Reconciler) scheduleInstance(logger logr.Logger, mgrID manager.ID, cfg multiinstanceai.Config) error {
	log.Debug(logger, "creating new instance", "manager_id", mgrID, "manager_config", cfg)
	if err := r.InstancesManager.ScheduleInstance(multiinstanceai.NewInstance(mgrID, logger, cfg)); err != nil {
		return fmt.Errorf("failed to schedule instance: %w", err)
	}
	return nil
}

// initStatusToWaitingToBecomeReady marks the resource as not ready yet and requeues it so that the
// instance's readiness is re-checked once it had a chance to boot.
func (r *Reconciler) initStatusToWaitingToBecomeReady(
	ctx context.Context,
	logger logr.Logger,
	onprem *aigatewayv1alpha1.OnPremAIGateway,
) (ctrl.Result, error) {
	k8sutils.SetCondition(
		k8sutils.NewCondition(
			aigatewayv1alpha1.ReadyType,
			metav1.ConditionFalse,
			aigatewayv1alpha1.WaitingToBecomeReadyReason,
			aigatewayv1alpha1.WaitingToBecomeReadyMessage,
		),
		onprem,
	)
	if err := r.applyStatus(ctx, logger, onprem); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfterBoot}, nil
}

// applyStatus patches the OnPremAIGateway status subresource via SSA.
func (r *Reconciler) applyStatus(ctx context.Context, logger logr.Logger, onprem *aigatewayv1alpha1.OnPremAIGateway) error {
	result, err := controllerpkgssa.ApplyStatusIfChanged(ctx, logger, r.Client, r.TypeConverter, onprem, controllerpkgssa.FieldManager)
	if err != nil {
		log.Error(logger, err, "failed to patch OnPremAIGateway status")
		return err
	}
	if result == op.Updated {
		log.Debug(logger, "OnPremAIGateway status updated")
	}
	return nil
}
