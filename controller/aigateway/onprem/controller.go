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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
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

	// NOTE: readiness only, no config assembly or push yet.
	// TODO: https://github.com/Kong/kong-operator/issues/5569
	k8sutils.SetReadyWithGeneration(onprem, onprem.Generation)

	if err := r.applyStatus(ctx, logger, onprem); err != nil {
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for OnPremAIGateway resource")
	return ctrl.Result{}, nil
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
