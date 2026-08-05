package mcpserver

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	konnectcontroller "github.com/kong/kong-operator/v2/controller/konnect"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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

func enqueueMCPServerForMCPServerDataPlane(cl client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		mcp, ok := obj.(*konnectv1alpha1.MCPServer)
		if !ok {
			return nil
		}
		mcpServerDataPlanes, err := listMCPServerDataPlanesForMCPServer(ctx, cl, mcp)
		if err != nil {
			return nil
		}

		reqs := make([]reconcile.Request, 0, len(mcpServerDataPlanes))
		for _, mcpdp := range mcpServerDataPlanes {
			reqs = append(reqs,
				reconcile.Request{
					NamespacedName: k8stypes.NamespacedName{
						Namespace: mcpdp.Namespace,
						Name:      mcpdp.Name,
					},
				},
			)
		}
		return reqs
	}
}

func listMCPServerDataPlanesForMCPServer(ctx context.Context, cl client.Client, mcp *konnectv1alpha1.MCPServer) ([]mcpv1alpha1.MCPServerDataPlane, error) {
	var l mcpv1alpha1.MCPServerDataPlaneList
	err := cl.List(
		ctx, &l,
		// NOTE: Currently only the same namespace references are supported.
		client.InNamespace(mcp.Namespace),
		client.MatchingFields{
			index.IndexFieldMCPServerOnMCPServerDataPlane: mcp.Namespace + "/" + mcp.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	return l.Items, nil
}

// reconcileEventHandler maps MCPServer events -- both real watch events and the
// synthetic ones pushed on ReconcileEventCh -- to the MCPServerDataPlanes that
// reference them. Requests must be keyed by the MCPServerDataPlane, not by the
// MCPServer, since this controller reconciles MCPServerDataPlane objects.
func (r *MCPServerDataPlaneReconciler) reconcileEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(enqueueMCPServerForMCPServerDataPlane(r.Client))
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
		Watches(&konnectv1alpha1.MCPServer{}, r.reconcileEventHandler()).
		WatchesRawSource(source.Channel(r.ReconcileEventCh, r.reconcileEventHandler())).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile reconciles the MCPServer resource.
func (r *MCPServerDataPlaneReconciler) Reconcile(ctx context.Context, mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	// Nothing to do on deletion: owned Deployment/Service/Kong entities are
	// cleaned up by ownerReference garbage collection. The signal-polling
	// offset reset lives on the mirrored MCPServer's own lifecycle, handled by
	// MCPServerSignalReconciler, not here.
	if !mcpDataPlane.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	log.Info(logger, "reconciling MCPServer", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)

	// Resolve the referenced MCPServer mirror entity to get the Konnect and
	// ControlPlane IDs assigned to it.
	ref := mcpDataPlane.Spec.MCPServerRef.KonnectNamespacedRef
	if ref == nil {
		return ctrl.Result{}, fmt.Errorf("MCPServerDataPlane %s/%s has no konnectNamespacedRef set",
			mcpDataPlane.Namespace, mcpDataPlane.Name)
	}

	var mcpServer konnectv1alpha1.MCPServer
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: mcpDataPlane.Namespace,
		Name:      ref.Name,
	}, &mcpServer); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get MCPServer %s/%s: %w", mcpDataPlane.Namespace, ref.Name, err)
	}

	mcpServerID := mcpServer.GetKonnectID()
	if mcpServerID == "" {
		log.Debug(logger, "Waiting for the MCPServer to get the ID assigned", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)
		return ctrl.Result{}, nil
	}
	cpID := mcpServer.GetControlPlaneID()
	if cpID == "" {
		log.Debug(logger, "Waiting for the MCPServer to get the ControlPlane ID assigned", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)
		return ctrl.Result{}, nil
	}

	// Resolve the reference chain to KonnectAPIAuthConfiguration and build the SDK.
	apiAuth, err := r.resolveAuth(ctx, &mcpServer)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to resolve auth for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}

	sdk, err := r.buildSDK(ctx, apiAuth)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build SDK for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}

	// Fetch the remote MCPServer from Konnect by its ID.
	resp, err := sdk.GetMCPServersSDK().GetMcpServerByControlPlane(ctx, cpID, mcpServerID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get MCPServer %s/%s from Konnect: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}
	if resp == nil || resp.MCPServerCPInfo == nil {
		return ctrl.Result{}, fmt.Errorf("got nil response for MCPServer %s/%s from Konnect",
			mcpDataPlane.Namespace, mcpDataPlane.Name)
	}
	remoteMCPServer := resp.MCPServerCPInfo

	mcpServerMetadata := mcpServerMetadata{
		ID:                 remoteMCPServer.ID,
		ContainerImage:     derefImage(remoteMCPServer.Container),
		InitContainerImage: derefImage(remoteMCPServer.InitContainer),
		Version:            remoteMCPServer.Version,
		ControlPlaneID:     cpID,
		MCPServerID:        mcpServerID,
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
	if err := r.ensureKongEntities(ctx, mcpDataPlane, sdk, cpID, mcpServerID, mcpServer.Spec.ControlPlaneRef); err != nil {
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

	// Patch the MCPServerDataPlane status from the live Deployment.
	old := mcpDataPlane.DeepCopy()
	ensureDataPlaneStatus(mcpDataPlane, deployment, mcpServerMetadata.Version)
	statusRes, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, mcpDataPlane, old)
	if err != nil {
		return ctrl.Result{}, err
	}
	if statusRes != op.Noop {
		log.Info(logger, "patched MCPServer status", "namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name)
	}

	return ctrl.Result{}, nil
}

// resolveAuth resolves the KonnectAPIAuthConfiguration for the given MCPServer,
// via the auth chain rooted at the MCPServer's ControlPlaneRef.
func (r *MCPServerDataPlaneReconciler) resolveAuth(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
) (*konnectv1alpha1.KonnectAPIAuthConfiguration, error) {
	apiAuthRef, err := konnectcontroller.GetAPIAuthRefNN(ctx, r.Client, mcpServer)
	if err != nil {
		return nil, fmt.Errorf("failed to get APIAuth ref: %w", err)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.Get(ctx, apiAuthRef, &apiAuth); err != nil {
		return nil, fmt.Errorf("failed to get KonnectAPIAuthConfiguration %s: %w", apiAuthRef, err)
	}

	return &apiAuth, nil
}

// buildSDK returns an authenticated SDK wrapper for the given, already-resolved
// KonnectAPIAuthConfiguration.
func (r *MCPServerDataPlaneReconciler) buildSDK(
	ctx context.Context,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (sdkops.SDKWrapper, error) {
	token, err := konnectcontroller.GetTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, apiAuth)
	if err != nil {
		return nil, fmt.Errorf("failed to get token from KonnectAPIAuthConfiguration %s/%s: %w",
			apiAuth.Namespace, apiAuth.Name, err)
	}

	srv, err := server.NewServer[konnectv1alpha1.MCPServer](apiAuth.Spec.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server URL from KonnectAPIAuthConfiguration %s/%s: %w",
			apiAuth.Namespace, apiAuth.Name, err)
	}

	return r.SdkFactory.NewKonnectSDK(srv, sdkops.SDKToken(token)), nil
}
