package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/go-logr/logr"
	"github.com/jpillora/backoff"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// MCPServersFetcher asynchronously fetches all MCP servers for a given control
// plane. It blocks on a wakeup channel and, upon receiving a signal, retrieves
// the full list of MCP servers from the Konnect API using
// ListMcpServersByControlPlane, paginating as needed, and creates a mirrored
// MCPServer Kubernetes object for each one.
type MCPServersFetcher struct {
	loggingMode logging.Mode

	client        client.Client
	scheme        *runtime.Scheme
	konnectClient sdkops.SDKWrapper

	controlPlane *konnectv1alpha2.KonnectGatewayControlPlane

	fetchEventCh     chan struct{}
	reconcileEventCh chan<- event.GenericEvent

	// lastSignal holds the most recent Konnect MCP signal notified via
	// NotifySignal, or nil if none has arrived yet. It is consumed and
	// propagated to mirrored MCPServers on the next fetch/sync pass.
	lastSignal atomic.Pointer[mcpSignal]
}

// mcpSignal carries the last-seen offset/version pair from a Konnect MCP
// signal (see signal.go), to be stamped onto mirrored MCPServer objects so
// that MCPServerDataPlaneReconciler can trigger a redeploy on change.
type mcpSignal struct {
	Offset  string
	Version string
}

// NewMCPServersFetcher creates a new MCPServersFetcher.
func NewMCPServersFetcher(
	loggingMode logging.Mode,
	cl client.Client,
	konnectClient sdkops.SDKWrapper,
	fetchEventCh chan struct{},
	reconcileEventCh chan<- event.GenericEvent,
	controlPlane *konnectv1alpha2.KonnectGatewayControlPlane,
	scheme *runtime.Scheme,
) *MCPServersFetcher {
	return &MCPServersFetcher{
		loggingMode:      loggingMode,
		client:           cl,
		konnectClient:    konnectClient,
		fetchEventCh:     fetchEventCh,
		reconcileEventCh: reconcileEventCh,
		controlPlane:     controlPlane,
		scheme:           scheme,
	}
}

// NotifySignal records sig as the latest Konnect MCP signal seen and wakes the
// fetch loop. The last signal wins: an older pending one is simply overwritten.
func (f *MCPServersFetcher) NotifySignal(sig mcpSignal) {
	f.lastSignal.Store(&sig)
	f.wake()
}

// wake sends a non-blocking wakeup on fetchEventCh: if a fetch is already
// pending, the extra notification is safely dropped.
func (f *MCPServersFetcher) wake() {
	select {
	case f.fetchEventCh <- struct{}{}:
	default:
	}
}

// run starts the background goroutine that waits for wakeup signals and fetches
// all MCP servers for the configured control plane.
// It returns when ctx is cancelled or the wakeup channel is closed.
// On a sync failure the wakeup is requeued after an exponential backoff delay.
func (f *MCPServersFetcher) run(ctx context.Context) {
	go func() {
		logger := log.GetLogger(ctx, "mcpserver-fetcher", f.loggingMode)
		b := &backoff.Backoff{
			Min:    time.Second,
			Max:    time.Minute,
			Factor: 2,
		}

		cpID := f.controlPlane.GetKonnectID()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-f.fetchEventCh:
				if !ok {
					return
				}
				servers, err := f.fetchAll(ctx)
				if err != nil {
					log.Error(logger, err, "failed to fetch MCP servers", "controlPlaneID", cpID)
					continue
				}
				log.Debug(logger, "fetched MCP servers", "controlPlaneID", cpID, "count", len(servers))
				sig := f.lastSignal.Load()
				if err := f.syncMCPServers(ctx, servers, sig); err != nil {
					log.Error(logger, err, "failed to sync MCP servers", "controlPlaneID", cpID)
					time.AfterFunc(b.Duration(), f.wake)
				} else {
					b.Reset()
				}
			}
		}
	}()
}

// syncMCPServers reconciles the mirrored/user-owned MCPServer Kubernetes
// objects for the control plane against the list of servers returned by
// Konnect:
//   - For a server with no matching in-cluster MCPServer, a mirror is created
//     if the server is in basic mode. Advanced-mode servers with no in-cluster
//     counterpart are left alone: users are expected to create and manage
//     their own MCPServer for advanced mode.
//   - For a server with a matching in-cluster MCPServer (whatever its mode,
//     whether operator-created or user-created), the latest signal (if any) is
//     stamped on it and a reconciliation is triggered.
//   - In-cluster MCPServers for this control plane whose Konnect counterpart
//     is no longer reported are deleted.
//
// All errors are collected and returned as a single joined error.
func (f *MCPServersFetcher) syncMCPServers(ctx context.Context, servers []sdkkonnectcomp.MCPServerCPInfo, sig *mcpSignal) error {
	var (
		errs        []error
		cpName      = f.controlPlane.Name
		cpNamespace = f.controlPlane.Namespace
		nnCP        = types.NamespacedName{
			Namespace: cpNamespace,
			Name:      cpName,
		}
		logger     = log.GetLogger(ctx, "mcpserver-fetcher", f.loggingMode)
		konnectIDs = make(map[string]struct{}, len(servers))
	)

	var existing konnectv1alpha1.MCPServerList
	if err := f.client.List(ctx, &existing,
		client.InNamespace(nnCP.Namespace),
		client.MatchingFields{
			index.IndexFieldMCPServerOnKonnectGatewayControlPlane: nnCP.String(),
		},
	); err != nil {
		return fmt.Errorf("failed to list MCPServers for control plane %s: %w", nnCP, err)
	}

	byID := make(map[string]*konnectv1alpha1.MCPServer, len(existing.Items))
	for i := range existing.Items {
		key := string(existing.Items[i].Spec.Mirror.Konnect.ID)
		if _, ok := byID[key]; ok {
			logger.Info("Duplicate MCPServers detected using the same Konnect ID. "+
				"Currently maximum of 1 is supported to use the same ID at a time",
				"konnectID", key,
			)
		}
		byID[key] = &existing.Items[i]
	}

	for _, server := range servers {
		// Presence in Konnect is what keeps an in-cluster MCPServer alive,
		// whatever its mode: record it before anything else so
		// cleanupMCPServersForControlPlane never deletes a server Konnect
		// still reports.
		konnectIDs[server.ID] = struct{}{}

		if err := f.syncMCPServer(ctx, logger, server, nnCP, byID[server.ID], sig); err != nil {
			errs = append(errs, err)
		}
	}

	if err := f.cleanupMCPServersForControlPlane(ctx, logger, existing.Items, konnectIDs); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// fetchAll retrieves all MCP servers for the control plane by paginating through
// all pages returned by ListMcpServersByControlPlane, retrying with exponential
// backoff on transient errors.
func (f *MCPServersFetcher) fetchAll(ctx context.Context) ([]sdkkonnectcomp.MCPServerCPInfo, error) {
	logger := log.GetLogger(ctx, "mcpserver-fetcher", f.loggingMode)
	b := &backoff.Backoff{
		Min:    time.Second,
		Max:    time.Minute,
		Factor: 2,
	}

	cpID := f.controlPlane.GetKonnectID()

	var (
		servers   []sdkkonnectcomp.MCPServerCPInfo
		pageAfter *string
	)

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp, err := f.konnectClient.GetMCPServersSDK().ListMcpServersByControlPlane(ctx,
			sdkkonnectops.ListMcpServersByControlPlaneRequest{
				ControlPlaneID: cpID,
				PageAfter:      pageAfter,
			},
		)
		if err != nil {
			log.Error(logger, err, "failed to list MCP servers by control plane, retrying",
				"controlPlaneID", cpID)
			select {
			case <-time.After(b.Duration()):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		b.Reset()

		if resp.StatusCode != http.StatusOK || resp.ListMCPServersCPInfoResponse == nil {
			break
		}

		servers = append(servers, resp.ListMCPServersCPInfoResponse.Data...)

		next := resp.ListMCPServersCPInfoResponse.Meta.Page.GetNext()
		if next == nil {
			break
		}
		pageAfter = next
	}

	return servers, nil
}

// syncMCPServer syncs a single Konnect MCP server to Kubernetes: if a
// matching in-cluster MCPServer exists (whether operator-created or
// user-created, in either mode), it is annotated with the latest signal (if
// any) and a reconciliation is triggered. Otherwise, a mirror is created for
// basic-mode servers; advanced-mode servers with no in-cluster counterpart are
// left for the user to create and configure.
func (f *MCPServersFetcher) syncMCPServer(
	ctx context.Context,
	logger logr.Logger,
	mcpServerInfo sdkkonnectcomp.MCPServerCPInfo,
	nnCP types.NamespacedName,
	existing *konnectv1alpha1.MCPServer,
	sig *mcpSignal,
) error {
	if existing != nil {
		if err := f.setSignalAnnotations(ctx, logger, existing, sig); err != nil {
			return fmt.Errorf("failed to update signal annotations on MCPServer %s/%s: %w", existing.Namespace, existing.Name, err)
		}
		// Trigger a reconciliation so the controller can sync its state with
		// the remote without waiting for a CRD change.
		select {
		case f.reconcileEventCh <- event.GenericEvent{Object: existing}:
		default:
			return fmt.Errorf("trigger channel is full, failed to enqueue reconciliation for MCPServer %s/%s", existing.Namespace, existing.Name)
		}
		return nil
	}

	// No in-cluster MCPServer for this Konnect server. Only basic mode gets an
	// operator-created mirror; advanced mode is the user's to deploy and
	// configure as they see fit.
	// NOTE: This does not take into account migrating between modes: if a
	// server is migrated from advanced to basic, a mirror is created here; if
	// migrated from basic to advanced, the existing mirror is left in place
	// (and kept alive by the presence check in syncMCPServers) rather than
	// handed over to the user.
	// TODO: https://github.com/Kong/kong-operator/issues/5135
	if mcpServerInfo.Mode != nil &&
		*mcpServerInfo.Mode != sdkkonnectcomp.MCPServerCPInfoModeBasic {
		return nil
	}

	nn := generateMCPServerNN(nnCP.Namespace, nnCP.Name, mcpServerInfo.ID)
	mcpServer := generateMCPServer(nn, mcpServerInfo, nnCP.Name, sig)
	if err := controllerutil.SetControllerReference(f.controlPlane, mcpServer, f.scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on MCPServer %s: %w", nn, err)
	}
	if err := f.client.Create(ctx, mcpServer); err != nil {
		return fmt.Errorf("failed to create MCPServer %s: %w", nn, err)
	}
	nnMCP := client.ObjectKeyFromObject(mcpServer)
	log.Debug(logger, "created MCPServer", "name", nnMCP.Name, "namespace", nnMCP.Namespace, "id", mcpServerInfo.ID)

	// TODO: No self-healing if the MCPServer mirror is created but the following
	// MCPServerDataPlane create fails — permanent orphan.
	mcpServerDataPlane := generateMCPServerDataPlane(nn, mcpServer)
	if err := controllerutil.SetControllerReference(mcpServer, mcpServerDataPlane, f.scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on MCPServerDataPlane %s: %w", nn, err)
	}
	if err := f.client.Create(ctx, mcpServerDataPlane); err != nil {
		return fmt.Errorf("failed to create MCPServerDataPlane %s: %w", nn, err)
	}
	nnMCPDataPlane := client.ObjectKeyFromObject(mcpServerDataPlane)
	log.Debug(logger, "created MCPServerDataPlane", "name", nnMCPDataPlane.Name, "namespace", nnMCPDataPlane.Namespace)

	return nil
}

// setSignalAnnotations stamps sig's offset/version onto mcpServer's
// annotations and patches the object if anything changed. A nil sig, or a
// sig matching what's already stored, is a no-op.
func (f *MCPServersFetcher) setSignalAnnotations(ctx context.Context, logger logr.Logger, mcpServer *konnectv1alpha1.MCPServer, sig *mcpSignal) error {
	if sig == nil ||
		(mcpServer.Annotations[mcpSignalOffsetAnnotationKey] == sig.Offset &&
			mcpServer.Annotations[mcpSignalVersionAnnotationKey] == sig.Version) {
		return nil
	}

	old := mcpServer.DeepCopy()
	if mcpServer.Annotations == nil {
		mcpServer.Annotations = make(map[string]string, 2)
	}
	mcpServer.Annotations[mcpSignalOffsetAnnotationKey] = sig.Offset
	mcpServer.Annotations[mcpSignalVersionAnnotationKey] = sig.Version

	_, _, err := patch.ApplyPatchIfNotEmpty(ctx, f.client, logger, mcpServer, old, true)
	return err
}

// generateMCPServerNN builds a Kubernetes-safe NamespacedName for a mirrored
// MCPServer from the control plane name/namespace, server name, and Konnect server ID.
func generateMCPServerNN(cpNamespace, cpName, serverID string) types.NamespacedName {
	return generateHashedName(cpNamespace, cpName, serverID)
}

func generateMCPServer(
	nn types.NamespacedName,
	server sdkkonnectcomp.MCPServerCPInfo,
	cpName string,
	sig *mcpSignal,
) *konnectv1alpha1.MCPServer {
	mcpServer := &konnectv1alpha1.MCPServer{
		Name:      nn.Name,
		Namespace: nn.Namespace,
		Finalizers: []string{
			mcpServerFinalizer,
		},
		Spec: konnectv1alpha1.MCPServerSpec{
			Source: new(commonv1alpha1.EntitySourceMirror),
			Mirror: konnectv1alpha1.MirrorSpec{
				Konnect: konnectv1alpha1.MirrorKonnect{
					ID: commonv1alpha1.KonnectIDType(server.ID),
				},
			},
			ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: cpName,
				},
			},
		},
	}
	if sig != nil {
		mcpServer.Annotations = map[string]string{
			mcpSignalOffsetAnnotationKey:  sig.Offset,
			mcpSignalVersionAnnotationKey: sig.Version,
		}
	}
	return mcpServer
}

func generateMCPServerDataPlane(
	nn types.NamespacedName,
	mcp *konnectv1alpha1.MCPServer,
) *mcpv1alpha1.MCPServerDataPlane {
	return &mcpv1alpha1.MCPServerDataPlane{
		Name:      nn.Name,
		Namespace: nn.Namespace,
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type: mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{
					Name: mcp.Name,
				},
			},
			Deployment: &mcpv1alpha1.DeploymentOptions{
				Replicas: new(int32(1)),
			},
		},
	}
}

// cleanupMCPServersForControlPlane deletes any MCPServer Kubernetes objects
// (in the given, already-fetched list of MCPServers for the control plane)
// that are no longer present in the provided map of Konnect server IDs.
// MCPServerDataPlane objects are automatically deleted by ownerReference garbage collection.
// It returns a joined error if any deletions fail.
func (f *MCPServersFetcher) cleanupMCPServersForControlPlane(
	ctx context.Context,
	logger logr.Logger,
	existing []konnectv1alpha1.MCPServer,
	mcpServerKonnectIDs map[string]struct{},
) error {
	var errs []error

	for i := range existing {
		mcpServer := &existing[i]
		id := string(mcpServer.Spec.Mirror.Konnect.ID)
		nnMCP := client.ObjectKeyFromObject(mcpServer)

		if _, ok := mcpServerKonnectIDs[id]; ok {
			continue
		}

		// Konnect is authoritative here: it already told us the server is gone,
		// so drop our signal-reset finalizer before deleting to skip the reset.
		// Deletions triggered by anyone else (user, owner GC) keep the finalizer
		// and do reset the polling offset, via MCPServerSignalReconciler.
		if _, res, err := patch.WithoutFinalizer(ctx, f.client, mcpServer, mcpServerFinalizer); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove finalizer from stale MCPServer %s: %w", nnMCP.String(), err))
			continue
		} else if !res.IsZero() {
			// Conflict: the object changed under us, retry on the next sync.
			continue
		}

		// Delete the stale MCPServer: its Konnect counterpart is no longer
		// present in the servers Konnect reported for this control plane.
		err := f.client.Delete(ctx, mcpServer)
		if client.IgnoreNotFound(err) != nil {
			errs = append(errs, fmt.Errorf("failed to delete stale MCPServer %s: %w", nnMCP.String(), err))
			continue
		}
		if err == nil {
			log.Debug(logger, "deleted stale MCPServer", "name", nnMCP.Name, "namespace", nnMCP.Namespace, "id", id)
		}
	}

	return errors.Join(errs...)
}
