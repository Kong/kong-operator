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

// MCPServerReconciler reconciles a MCPServer object.
type MCPServerReconciler struct {
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

	// typeConverter is initialised once during SetupWithManager from the API
	// server's OpenAPI v3 schemas. It supports all types (core K8s + CRDs) and
	// is used for diff-before-apply via Server-Side Apply.
	typeConverter managedfields.TypeConverter

	// eventRecorder records Kubernetes events on MCPServer objects.
	eventRecorder events.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	tc, err := initTypeConverter(mgr)
	if err != nil {
		return fmt.Errorf("MCPServer controller: failed to initialize TypeConverter: %w", err)
	}
	r.typeConverter = tc
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&konnectv1alpha1.MCPServer{}).
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
		Complete(reconcile.AsReconciler[*konnectv1alpha1.MCPServer](r.Client, r))
}

// Reconcile reconciles the MCPServer resource.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, mcpServer *konnectv1alpha1.MCPServer) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	// Handle pre-deletion: notify the signal manager to reset the polling offset
	// so the next poll picks up any changes caused by the deletion, then remove
	// the finalizer to allow Kubernetes to garbage-collect the object.
	if !mcpServer.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(mcpServer, mcpServerFinalizer) {
			if cpName := ownerControlPlaneName(mcpServer); cpName != "" {
				r.SignalManager.NotifyMCPServerDeleted(mcpServer.Namespace, cpName)
			}
			controllerutil.RemoveFinalizer(mcpServer, mcpServerFinalizer)
			if err := r.Update(ctx, mcpServer); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from MCPServer %s/%s: %w", mcpServer.Namespace, mcpServer.Name, err)
			}
		}
		return ctrl.Result{}, nil
	}

	log.Info(logger, "reconciling MCPServer", "namespace", mcpServer.Namespace, "name", mcpServer.Name)

	if !controllerutil.ContainsFinalizer(mcpServer, mcpServerFinalizer) {
		controllerutil.AddFinalizer(mcpServer, mcpServerFinalizer)
		if err := r.Update(ctx, mcpServer); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer to MCPServer %s/%s: %w", mcpServer.Namespace, mcpServer.Name, err)
		}
		return ctrl.Result{}, nil
	}

	// Fetch the remote MCPServer from Konnect by its ID.
	mcpServerID := mcpServer.GetKonnectID()
	if mcpServerID == "" {
		log.Debug(logger, "Waiting for the MCPServer to get the ID assigned", "namespace", mcpServer.Namespace, "name", mcpServer.Name)
		return ctrl.Result{}, nil
	}
	cpID := mcpServer.GetControlPlaneID()
	if cpID == "" {
		log.Debug(logger, "Waiting for the MCPServer to get the ControlPlane ID assigned", "namespace", mcpServer.Namespace, "name", mcpServer.Name)
		return ctrl.Result{}, nil
	}

	// Resolve the reference chain to KonnectAPIAuthConfiguration and build the SDK.
	apiAuth, err := r.resolveAuth(ctx, mcpServer)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to resolve auth for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}

	sdk, err := r.buildSDK(ctx, mcpServer)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build SDK for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}

	resp, err := sdk.GetMCPServersSDK().GetMcpServerByControlPlane(ctx, cpID, mcpServerID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get MCPServer %s/%s from Konnect: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}
	if resp == nil || resp.MCPServerCPInfo == nil {
		return ctrl.Result{}, fmt.Errorf("got nil response for MCPServer %s/%s from Konnect",
			mcpServer.Namespace, mcpServer.Name)
	}
	remoteMCPServer := resp.MCPServerCPInfo

	// Ensure a Deployment exists for this MCPServer.
	deployment, err := r.ensureDeployment(ctx, logger, mcpServer, remoteMCPServer, apiAuth)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Ensure a Service exists for this MCPServer.
	if err := r.ensureService(ctx, logger, mcpServer); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure Kong entities (KongService, KongRoute) are created in the cluster
	// from the remote MCP server's entity definitions.
	if err := r.ensureKongEntities(ctx, mcpServer, sdk); err != nil {
		return ctrl.Result{}, err
	}

	// Gather per-version workload status and push it to Konnect.
	versionStatuses, err := buildVersionStatuses(ctx, r.Client, deployment)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build version statuses for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}
	log.Debug(logger, "posting MCPServer status to Konnect",
		"namespace", mcpServer.Namespace, "name", mcpServer.Name,
		"versionStatuses", versionStatuses,
	)
	if err := postStatusToKonnect(ctx, sdk, cpID, mcpServerID, versionStatuses); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to post status for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}

	// Patch the MCPServer status with the remote version.
	version := remoteMCPServer.Version
	old := mcpServer.DeepCopy()
	if mcpServer.Status.KonnectSpec == nil {
		mcpServer.Status.KonnectSpec = &konnectv1alpha1.MCPServerKonnectSpec{}
	}
	mcpServer.Status.KonnectSpec.Version = &version
	mcpServer.Status.KonnectSpec.Name = &remoteMCPServer.Name
	statusRes, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, mcpServer, old)
	if err != nil {
		return ctrl.Result{}, err
	}
	if statusRes != op.Noop {
		log.Info(logger, "patched MCPServer status", "namespace", mcpServer.Namespace, "name", mcpServer.Name)
	}

	return ctrl.Result{}, nil
}
