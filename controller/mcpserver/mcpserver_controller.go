package mcpserver

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// MCPServerReconciler reconciles a MCPServer object.
type MCPServerReconciler struct {
	client.Client

	ControllerOptions controller.Options
	LoggingMode       logging.Mode
	SignalManager     *SignalManager
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.SignalManager.run(ctx)

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&konnectv1alpha1.MCPServer{}).
		Complete(r)
}

// Reconcile reconciles the MCPServer resource.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	var mcpServer konnectv1alpha1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &mcpServer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle pre-deletion: notify the signal manager to reset the polling offset
	// so the next poll picks up any changes caused by the deletion, then remove
	// the finalizer to allow Kubernetes to garbage-collect the object.
	if !mcpServer.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&mcpServer, mcpServerFinalizer) {
			if cpName := ownerControlPlaneName(&mcpServer); cpName != "" {
				r.SignalManager.NotifyMCPServerDeleted(mcpServer.Namespace, cpName)
			}
			controllerutil.RemoveFinalizer(&mcpServer, mcpServerFinalizer)
			if err := r.Update(ctx, &mcpServer); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from MCPServer %s/%s: %w", mcpServer.Namespace, mcpServer.Name, err)
			}
		}
		return ctrl.Result{}, nil
	}

	log.Info(logger, "reconciling MCPServer", "namespace", mcpServer.Namespace, "name", mcpServer.Name)

	return ctrl.Result{}, nil
}

// ownerControlPlaneName returns the name of the KonnectGatewayControlPlane that
// owns the given MCPServer, or an empty string if no such owner is found.
func ownerControlPlaneName(mcpServer *konnectv1alpha1.MCPServer) string {
	for _, ref := range mcpServer.OwnerReferences {
		if ref.Kind == "KonnectGatewayControlPlane" {
			return ref.Name
		}
	}
	return ""
}
