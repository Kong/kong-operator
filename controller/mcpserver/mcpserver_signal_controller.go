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

package mcpserver

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

const (
	// mcpServerFinalizer is added to every mirrored MCPServer so that
	// MCPServerSignalReconciler can reset the signal-polling offset before the
	// object is garbage-collected.
	mcpServerFinalizer = "kong-operator.konghq.com/mcp-server-signal-cleanup"
)

// MCPServerSignalReconciler reconciles a mirrored konnectv1alpha1.MCPServer to
// keep the signal-polling offset in sync with its lifecycle: it adds
// mcpServerFinalizer to every mirror and, on deletion, notifies the
// SignalManager to reset the offset before letting the object go.
//
// This is deliberately separate from MCPServerDataPlaneReconciler: the reset
// must react to the mirrored MCPServer disappearing, not to the DataPlane
// workload being torn down.
type MCPServerSignalReconciler struct {
	client.Client

	ControllerOptions controller.Options
	LoggingMode       logging.Mode
	SignalManager     *SignalManager
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerSignalReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&konnectv1alpha1.MCPServer{}).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile reconciles the mirrored MCPServer resource.
func (r *MCPServerSignalReconciler) Reconcile(ctx context.Context, mcpServer *konnectv1alpha1.MCPServer) (ctrl.Result, error) {
	// Only mirrors owned by a KonnectGatewayControlPlane take part in signal
	// polling; anything else has no offset to reset.
	cpName := ownerControlPlaneName(mcpServer)
	if cpName == "" {
		return ctrl.Result{}, nil
	}

	if !mcpServer.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(mcpServer, mcpServerFinalizer) {
			// Notify before removing the finalizer: the reset must not be lost if
			// the process dies mid-way. Repeat notifications are harmless --
			// resetCh is buffered size 1 and coalesces (see signal.go).
			r.SignalManager.NotifyMCPServerDeleted(mcpServer.Namespace, cpName)
			if _, res, err := patch.WithoutFinalizer(ctx, r.Client, mcpServer, mcpServerFinalizer); err != nil || !res.IsZero() {
				return res, err
			}
		}
		return ctrl.Result{}, nil
	}

	_, res, err := patch.WithFinalizer(ctx, r.Client, mcpServer, mcpServerFinalizer)
	return res, err
}
