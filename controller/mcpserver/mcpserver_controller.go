package mcpserver

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

const (
	// TriggerChannelBufSize is the buffer size for the channel used to
	// enqueue artificial reconciliation events.
	TriggerChannelBufSize = 100
)

// MCPServerDataPlaneReconciler reconciles a MCPServerDataPlane object.
type MCPServerDataPlaneReconciler struct {
	client.Client

	Scheme *runtime.Scheme

	ControllerOptions controller.Options
	LoggingMode       logging.Mode
	SignalManager     *SignalManager
	SdkFactory        sdkops.SDKFactory

	ClusterDomain string

	// ReconcileEventCh allows external callers to push synthetic reconciliation
	// events so that a Reconcile loop starts without an actual change on the
	// MCPServer CRD.
	ReconcileEventCh chan event.GenericEvent

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time.  It is used for diff-before-apply via Server-Side Apply.
	TypeConverter managedfields.TypeConverter

	// eventRecorder records Kubernetes events on MCPServer objects.
	eventRecorder events.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerDataPlaneReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&mcpv1alpha1.MCPServerDataPlane{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&configurationv1alpha1.KongService{}).
		Owns(&configurationv1alpha1.KongRoute{}).
		Owns(&configurationv1.KongPlugin{}).
		Owns(&configurationv1alpha1.KongPluginBinding{}).
		WatchesRawSource(
			source.Channel(
				r.ReconcileEventCh,
				&handler.EnqueueRequestForObject{},
			),
		).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile reconciles the MCPServer resource.
func (r *MCPServerDataPlaneReconciler) Reconcile(ctx context.Context, mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	// Handle pre-deletion: notify the signal manager to reset the polling offset
	// so the next poll picks up any changes caused by the deletion, then remove
	// the finalizer to allow Kubernetes to garbage-collect the object.
	if !mcpDataPlane.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(mcpDataPlane, mcpServerFinalizer) {
			// TODO(pmalek)
			// if cpName := ownerControlPlaneName(mcpServer); cpName != "" {
			// 	r.SignalManager.NotifyMCPServerDeleted(mcpServer.Namespace, cpName)
			// }
			controllerutil.RemoveFinalizer(mcpDataPlane, mcpServerFinalizer)
			if err := r.Update(ctx, mcpDataPlane); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from MCPServer %s/%s: %w", mcpDataPlane.Namespace, mcpDataPlane.Name, err)
			}
		}
		return ctrl.Result{}, nil
	}

	log.Info(logger, "reconciling MCPServer", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)

	if !controllerutil.ContainsFinalizer(mcpDataPlane, mcpServerFinalizer) {
		controllerutil.AddFinalizer(mcpDataPlane, mcpServerFinalizer)
		if err := r.Update(ctx, mcpDataPlane); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer to MCPServer %s/%s: %w", mcpDataPlane.Namespace, mcpDataPlane.Name, err)
		}
		return ctrl.Result{}, nil
	}

	// Fetch the remote MCPServer from Konnect by its ID.

	// mcpServerID := mcpDataPlane.GetKonnectID()
	// if mcpServerID == "" {
	// 	log.Debug(logger, "Waiting for the MCPServer to get the ID assigned", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)
	// 	return ctrl.Result{}, nil
	// }
	// cpID := mcpDataPlane.GetControlPlaneID()
	// if cpID == "" {
	// 	log.Debug(logger, "Waiting for the MCPServer to get the ControlPlane ID assigned", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)
	// 	return ctrl.Result{}, nil
	// }
	//
	// TODO(pmalek):
	var mcpServer konnectv1alpha1.MCPServer
	err := r.Get(ctx, client.ObjectKey{Namespace: mcpDataPlane.Namespace, Name: mcpDataPlane.Name}, &mcpServer)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get MCPServer %s: %w", client.ObjectKeyFromObject(mcpDataPlane), err)
	}
	mcpServerID := mcpServer.GetKonnectID()
	if mcpServerID == "" {
		log.Debug(logger, "Waiting for the MCPServer to get the ID assigned", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)
		return ctrl.Result{}, nil
	}
	cpID := mcpServer.Status.ControlPlaneID

	// Resolve the reference chain to KonnectAPIAuthConfiguration and build the SDK.
	apiAuth, err := r.resolveAuth(ctx, mcpDataPlane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to resolve auth for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}

	sdk, err := r.buildSDK(ctx, mcpDataPlane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build SDK for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}

	mcpServerMetadata := mcpServerMetadata{
		// TODO(pmalek)
		ContainerImage:     "",
		InitContainerImage: "",
		Version:            "",
	}

	// Ensure a Deployment exists for this MCPServer.
	deployment, err := r.ensureDeployment(ctx, logger, mcpDataPlane, mcpServerMetadata, apiAuth)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Ensure a Service exists for this MCPServer.
	if err := r.ensureService(ctx, logger, mcpDataPlane); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure Kong entities (KongService, KongRoute) are created in the cluster
	// from the remote MCP server's entity definitions.
	if err := r.ensureKongEntities(ctx, mcpDataPlane, sdk); err != nil {
		return ctrl.Result{}, err
	}

	// Gather per-version workload status and push it to Konnect.
	versionStatuses, err := buildVersionStatuses(ctx, r.Client, deployment)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build version statuses for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}
	log.Debug(logger, "posting MCPServer status to Konnect",
		"namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name,
		"versionStatuses", versionStatuses,
	)
	if err := postStatusToKonnect(ctx, sdk, cpID, mcpServerID, versionStatuses); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to post status for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}

	// Patch the MCPServer status with the remote version.
	old := mcpDataPlane.DeepCopy()
	mcpDataPlane.Status.Version = mcpServerMetadata.Version
	statusRes, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, mcpDataPlane, old)
	if err != nil {
		return ctrl.Result{}, err
	}
	if statusRes != op.Noop {
		log.Info(logger, "patched MCPServer status", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)
	}

	return ctrl.Result{}, nil
}
