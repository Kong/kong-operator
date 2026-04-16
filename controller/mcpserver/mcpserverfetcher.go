package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/jpillora/backoff"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
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

const (
	// mcpServerFinalizer is added to every mirrored MCPServer so that the
	// MCPServerReconciler can reset the signal-polling offset before the object
	// is garbage-collected.
	mcpServerFinalizer = "kong-operator.konghq.com/mcp-server-signal-cleanup"
)

// syncMCPServers creates a mirrored MCPServer Kubernetes object for each server
// returned by Konnect. Already-existing objects are skipped silently.
// Objects that exist in Kubernetes but are no longer present in Konnect are deleted.
// All errors are collected and returned as a single joined error.
func (f *MCPServersFetcher) syncMCPServers(ctx context.Context, servers []sdkkonnectcomp.MCPServerCPInfo) error {
	logger := log.GetLogger(ctx, "mcpserver-fetcher", f.loggingMode)
	var errs []error

	cpName := f.controlPlane.Name
	cpNamespace := f.controlPlane.Namespace

	konnectIDs := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		if server.ResourceID == nil || *server.ResourceID == "" {
			// The server is not fully provisioned on the Konnect side.
			// Delete the corresponding Kubernetes resource if it exists.
			nn := generateMCPServerNN(cpNamespace, cpName, server.ID)
			mcpServer := &konnectv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nn.Name,
					Namespace: nn.Namespace,
				},
			}
			if err := client.IgnoreNotFound(f.client.Delete(ctx, mcpServer)); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete MCPServer without resource ID %s: %w", nn, err))
			}
			continue
		}
		konnectIDs[server.ID] = struct{}{}

		nn := generateMCPServerNN(cpNamespace, cpName, server.ID)
		var existing konnectv1alpha1.MCPServer
		if err := f.client.Get(ctx, nn, &existing); err == nil {
			// The MCPServer already exists on the API server: trigger a
			// reconciliation so the controller can sync its state with the
			// remote without waiting for a CRD change.
			select {
			case f.reconcileEventCh <- event.GenericEvent{Object: &existing}:
			default:
				errs = append(errs, fmt.Errorf("trigger channel is full, failed to enqueue reconciliation for MCPServer %s/%s", cpNamespace, existing.Name))
			}
			continue
		} else if !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("failed to check MCPServer existence %s: %w", nn, err))
			continue
		}

		mcpServer := &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
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

		if err := controllerutil.SetControllerReference(f.controlPlane, mcpServer, f.scheme); err != nil {
			errs = append(errs, fmt.Errorf("failed to set owner reference on MCPServer %s: %w", nn, err))
			continue
		}
		err := f.client.Create(ctx, mcpServer)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create MCPServer %s: %w", nn, err))
			continue
		}
		log.Debug(logger, "created MCPServer", "id", server.ID, "namespace", cpNamespace)
	}

	// Delete MCPServers that exist in Kubernetes but are no longer present in Konnect.
	var existing konnectv1alpha1.MCPServerList
	if err := f.client.List(ctx, &existing,
		client.InNamespace(cpNamespace),
		client.MatchingFields{index.IndexFieldMCPServerOnKonnectGatewayControlPlane: cpNamespace + "/" + cpName},
	); err != nil {
		errs = append(errs, fmt.Errorf("failed to list MCPServers for control plane %s/%s: %w", cpNamespace, cpName, err))
		return errors.Join(errs...)
	}

	for i := range existing.Items {
		mcpServer := &existing.Items[i]
		id := string(mcpServer.Spec.Mirror.Konnect.ID)
		if _, ok := konnectIDs[id]; ok {
			continue
		}

		// Delete MCPServers whose Konnect counterpart no longer exists or
		// whose MCPServerCPInfo has no ResourceID (the server is not fully
		// provisioned on the Konnect side).
		err := f.client.Delete(ctx, mcpServer)
		if client.IgnoreNotFound(err) != nil {
			errs = append(errs, fmt.Errorf("failed to delete stale MCPServer %s/%s: %w", cpNamespace, mcpServer.Name, err))
			continue
		}
		if err == nil {
			log.Debug(logger, "deleted stale MCPServer", "name", mcpServer.Name, "id", id, "namespace", cpNamespace)
		}
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

// generateMCPServerNN builds a Kubernetes-safe NamespacedName for a mirrored
// MCPServer from the control plane name/namespace, server name, and Konnect server ID.
func generateMCPServerNN(cpNamespace, cpName, serverID string) types.NamespacedName {
	return generateHashedName(cpNamespace, cpName, serverID)
}
