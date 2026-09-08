package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/go-logr/logr"
	"github.com/jpillora/backoff"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
				if err := f.syncMCPServers(ctx, servers); err != nil {
					log.Error(logger, err, "failed to sync MCP servers", "controlPlaneID", cpID)
					time.AfterFunc(b.Duration(), func() {
						select {
						case f.fetchEventCh <- struct{}{}:
						default:
						}
					})
				} else {
					b.Reset()
				}
			}
		}
	}()
}

// syncMCPServers creates a mirrored MCPServer Kubernetes object for each server
// returned by Konnect. Already-existing objects are skipped silently.
// Objects that exist in Kubernetes but are no longer present in Konnect are deleted.
// All errors are collected and returned as a single joined error.
func (f *MCPServersFetcher) syncMCPServers(ctx context.Context, servers []sdkkonnectcomp.MCPServerCPInfo) error {
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

	for _, server := range servers {
		ok, err := f.syncMCPServer(ctx, logger, server, nnCP, konnectIDs)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok {
			logger.Info("MCPServer failed to synchronize", "id", server.ID, "cp", nnCP)
			continue
		}
	}

	if err := f.cleanupMCPServersForControlPlane(ctx, logger, nnCP, konnectIDs); err != nil {
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

// syncMCPServer syncs MCP Server from Konnect to Kubernetes.
// It also deploys the MCPServerDataPlane for the MCPServer if it does not exist yet.
// It returns true if the sync was successful.
func (f *MCPServersFetcher) syncMCPServer(
	ctx context.Context,
	logger logr.Logger,
	mcpServerInfo sdkkonnectcomp.MCPServerCPInfo,
	nnCP types.NamespacedName,
	mcpServerKonnectIDs map[string]struct{},
) (bool, error) {

	// Skip servers that are not in "basic" mode. Currently, the only other
	// mode is advanced and we do not sync/deploy MCPServers for advanced mode.
	// Users are expected to deploy their own MCPServer for advanced mode
	// and configure it as they see fit.
	// NOTE: This does not take into account migrating between modes.
	// If a server is migrated from basic to advanced, the mirrored MCPServer will
	// be deleted.
	// If a server is migrated from advanced to basic, the mirrored
	// MCPServer will be created.
	// TODO: https://github.com/Kong/kong-operator/issues/5135
	if mcpServerInfo.Mode != nil &&
		*mcpServerInfo.Mode != sdkkonnectcomp.MCPServerCPInfoModeBasic {
		return true, nil
	}

	mcpServerKonnectIDs[mcpServerInfo.ID] = struct{}{}

	nn := generateMCPServerNN(nnCP.Namespace, nnCP.Name, mcpServerInfo.ID)
	var existing konnectv1alpha1.MCPServer
	if err := f.client.Get(ctx, nn, &existing); err == nil {
		// The MCPServer already exists on the API server: trigger a
		// reconciliation so the controller can sync its state with the
		// remote without waiting for a CRD change.
		select {
		case f.reconcileEventCh <- event.GenericEvent{Object: &existing}:
		default:
			return false, fmt.Errorf("trigger channel is full, failed to enqueue reconciliation for MCPServer %s/%s", nnCP.Namespace, existing.Name)
		}
		return false, nil
	} else if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to check MCPServer existence %s: %w", nn, err)
	}

	mcpServer := generateMCPServer(nn, mcpServerInfo, nnCP.Name)
	if err := controllerutil.SetControllerReference(f.controlPlane, mcpServer, f.scheme); err != nil {
		return false, fmt.Errorf("failed to set owner reference on MCPServer %s: %w", nn, err)
	}
	if err := f.client.Create(ctx, mcpServer); err != nil {
		return false, fmt.Errorf("failed to create MCPServer %s: %w", nn, err)
	}
	nnMCP := client.ObjectKeyFromObject(mcpServer)
	log.Debug(logger, "created MCPServer", "name", nnMCP.Name, "namespace", nnMCP.Namespace, "id", mcpServerInfo.ID)

	// TODO: No self-healing if the MCPServer mirror is created but the following
	// MCPServerDataPlane create fails — permanent orphan.
	mcpServerDataPlane := generateMCPServerDataPlane(nn, mcpServer)
	if err := controllerutil.SetControllerReference(mcpServer, mcpServerDataPlane, f.scheme); err != nil {
		return false, fmt.Errorf("failed to set owner reference on MCPServerDataPlane %s: %w", nn, err)
	}
	if err := f.client.Create(ctx, mcpServerDataPlane); err != nil {
		return false, fmt.Errorf("failed to create MCPServerDataPlane %s: %w", nn, err)
	}
	nnMCPDataPlane := client.ObjectKeyFromObject(mcpServerDataPlane)
	log.Debug(logger, "created MCPServerDataPlane", "name", nnMCPDataPlane.Name, "namespace", nnMCPDataPlane.Namespace)

	return true, nil
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
) *konnectv1alpha1.MCPServer {
	return &konnectv1alpha1.MCPServer{
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
// that exist for the given control plane but are no longer present in the provided
// map of Konnect server IDs.
// MCPServerDataPlane objects are automatically deleted by ownerReference garbage collection.
// It returns a joined error if any deletions fail.
func (f *MCPServersFetcher) cleanupMCPServersForControlPlane(
	ctx context.Context,
	logger logr.Logger,
	nnCP types.NamespacedName,
	mcpServerKonnectIDs map[string]struct{},
) error {
	var (
		errs        []error
		objectKeyCP = nnCP.String()
	)

	// Delete MCPServers that exist in Kubernetes but are no longer present in Konnect.
	var existing konnectv1alpha1.MCPServerList
	if err := f.client.List(ctx, &existing,
		client.InNamespace(nnCP.Namespace),
		client.MatchingFields{
			index.IndexFieldMCPServerOnKonnectGatewayControlPlane: objectKeyCP,
		},
	); err != nil {
		return fmt.Errorf("failed to list MCPServers for control plane %s: %w", objectKeyCP, err)
	}

	for i := range existing.Items {
		mcpServer := &existing.Items[i]
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
