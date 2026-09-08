package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestGenerateMCPServerName(t *testing.T) {
	hashOf := func(id string) string {
		h := sha256.Sum256([]byte(id))
		return hex.EncodeToString(h[:])[:8]
	}

	tests := []struct {
		name       string
		cpName     string
		serverName string
		serverID   string
	}{
		{
			name:       "short names stay under 63 chars",
			cpName:     "my-cp",
			serverName: "my-server",
			serverID:   "abc123",
		},
		{
			name:       "long prefix is truncated, hash preserved",
			cpName:     strings.Repeat("a", 40),
			serverName: strings.Repeat("b", 40),
			serverID:   "some-long-id",
		},
		{
			name:       "trailing hyphens from truncation are trimmed",
			cpName:     strings.Repeat("x", 50) + "---",
			serverName: strings.Repeat("y", 20),
			serverID:   "id-1",
		},
		{
			name:       "exact 63 chars without truncation",
			cpName:     strings.Repeat("c", 27),
			serverName: strings.Repeat("d", 26),
			serverID:   "exact-fit",
		},
		{
			name:       "deterministic: same inputs produce same output",
			cpName:     "cp",
			serverName: "srv",
			serverID:   "deterministic-test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateMCPServerNN("test-ns", tt.cpName, tt.serverID)

			assert.LessOrEqual(t, len(result.Name), 63, "name must not exceed 63 characters")
			assert.NotEmpty(t, result.Name)
			assert.Equal(t, "test-ns", result.Namespace)

			// The last 8 characters must always be the hash of the server ID.
			shortHash := hashOf(tt.serverID)
			assert.True(t, strings.HasSuffix(result.Name, shortHash),
				"name %q must end with hash %q", result.Name, shortHash)

			// Must not end with a hyphen before the hash (i.e. no double hyphens at the join).
			assert.NotContains(t, result.Name, "--",
				"name %q must not contain double hyphens", result.Name)

			// Determinism: calling again must produce the same result.
			assert.Equal(t, result, generateMCPServerNN("test-ns", tt.cpName, tt.serverID))
		})
	}
}

func TestSyncMCPServers(t *testing.T) {
	const (
		cpName    = "test-cp"
		cpID      = "cp-konnect-id"
		namespace = "default"
	)

	// controlPlane is the owner KonnectGatewayControlPlane used in all tests.
	controlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		Name:      cpName,
		Namespace: namespace,
		Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: cpID,
			},
		},
	}

	newServer := func(id, name string, resourceID *string) sdkkonnectcomp.MCPServerCPInfo {
		return sdkkonnectcomp.MCPServerCPInfo{
			ID:         id,
			Name:       name,
			ResourceID: resourceID,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	// existingMCPServer returns an MCPServer that matches what syncMCPServers would
	// check for, using generateMCPServerNN for the name and the given mirror ID.
	// It carries mcpServerFinalizer, matching every mirror the fetcher creates.
	existingMCPServer := func(serverID string) *konnectv1alpha1.MCPServer {
		nn := generateMCPServerNN(namespace, cpName, serverID)
		return &konnectv1alpha1.MCPServer{
			Name:       nn.Name,
			Namespace:  nn.Namespace,
			Finalizers: []string{mcpServerFinalizer},
			Spec: konnectv1alpha1.MCPServerSpec{
				Mirror: konnectv1alpha1.MirrorSpec{
					Konnect: konnectv1alpha1.MirrorKonnect{
						ID: commonv1alpha1.KonnectIDType(serverID),
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

	tests := []struct {
		name             string
		servers          []sdkkonnectcomp.MCPServerCPInfo
		existingObjects  []client.Object
		interceptFuncs   *interceptor.Funcs
		expectError      bool
		expectCreated    []string // MCPServer names expected to exist after sync
		expectDeleted    []string // MCPServer names expected to be gone after sync
		expectDataPlanes []string // MCPServerDataPlane names expected to exist after sync
	}{
		{
			name:          "no servers, no existing objects is a no-op",
			servers:       []sdkkonnectcomp.MCPServerCPInfo{},
			expectCreated: []string{},
		},
		{
			name:             "new server is created",
			servers:          []sdkkonnectcomp.MCPServerCPInfo{newServer("srv-id", "srv-name", new("resource-id"))},
			expectCreated:    []string{generateMCPServerNN(namespace, cpName, "srv-id").Name},
			expectDataPlanes: []string{generateMCPServerNN(namespace, cpName, "srv-id").Name},
		},
		{
			name:            "existing server (by ID) is skipped without re-creating",
			servers:         []sdkkonnectcomp.MCPServerCPInfo{newServer("srv-id", "srv-name", new("resource-id"))},
			existingObjects: []client.Object{existingMCPServer("srv-id")},
			expectCreated:   []string{generateMCPServerNN(namespace, cpName, "srv-id").Name},
		},
		{
			name: "stale MCPServer not in Konnect response is deleted",
			servers: []sdkkonnectcomp.MCPServerCPInfo{
				newServer("live-id", "live-name", new("resource-id")),
			},
			existingObjects: []client.Object{
				existingMCPServer("live-id"),
				existingMCPServer("stale-id"),
			},
			expectCreated: []string{generateMCPServerNN(namespace, cpName, "live-id").Name},
			expectDeleted: []string{generateMCPServerNN(namespace, cpName, "stale-id").Name},
		},
		{
			name: "mixed: creates new, keeps existing, deletes stale",
			servers: []sdkkonnectcomp.MCPServerCPInfo{
				newServer("existing-id", "existing-name", new("resource-id")),
				newServer("new-id", "new-name", new("resource-id")),
			},
			existingObjects: []client.Object{
				existingMCPServer("existing-id"),
				existingMCPServer("stale-id"),
			},
			expectCreated: []string{
				generateMCPServerNN(namespace, cpName, "existing-id").Name,
				generateMCPServerNN(namespace, cpName, "new-id").Name,
			},
			expectDeleted:    []string{generateMCPServerNN(namespace, cpName, "stale-id").Name},
			expectDataPlanes: []string{generateMCPServerNN(namespace, cpName, "new-id").Name},
		},
		{
			name:    "list error is returned",
			servers: []sdkkonnectcomp.MCPServerCPInfo{},
			interceptFuncs: &interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*konnectv1alpha1.MCPServerList); ok {
						return fmt.Errorf("simulated list failure")
					}
					return c.List(ctx, list, opts...)
				},
			},
			expectError: true,
		},
		{
			name:             "new server is created without a resource ID assigned",
			servers:          []sdkkonnectcomp.MCPServerCPInfo{newServer("srv-id", "srv-name", nil)},
			expectCreated:    []string{generateMCPServerNN(namespace, cpName, "srv-id").Name},
			expectDataPlanes: []string{generateMCPServerNN(namespace, cpName, "srv-id").Name},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := scheme.Get()
			builder := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(controlPlane).
				WithIndex(&konnectv1alpha1.MCPServer{},
					index.IndexFieldMCPServerOnKonnectGatewayControlPlane,
					func(obj client.Object) []string {
						mcp, ok := obj.(*konnectv1alpha1.MCPServer)
						if !ok {
							return nil
						}
						ref := mcp.Spec.ControlPlaneRef
						if ref.KonnectNamespacedRef == nil {
							return nil
						}
						return []string{mcp.Namespace + "/" + ref.KonnectNamespacedRef.Name}
					},
				)

			if len(tt.existingObjects) > 0 {
				builder = builder.WithObjects(tt.existingObjects...)
			}
			if tt.interceptFuncs != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptFuncs)
			}
			cl := builder.Build()

			reconcileEventCh := make(chan event.GenericEvent, TriggerChannelBufSize)
			f := NewMCPServersFetcher(logging.DevelopmentMode, cl, nil, make(chan struct{}, 1), reconcileEventCh, controlPlane, s)

			err := f.syncMCPServers(t.Context(), tt.servers)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify expected objects exist and carry the signal-reset finalizer,
			// so a subsequent stale-delete is always intercepted by
			// MCPServerSignalReconciler rather than deleted outright.
			for _, expectedName := range tt.expectCreated {
				var mcp konnectv1alpha1.MCPServer
				require.NoError(t,
					cl.Get(t.Context(), client.ObjectKey{Name: expectedName, Namespace: namespace}, &mcp),
					"expected MCPServer %q to exist", expectedName,
				)
				assert.Contains(t, mcp.Finalizers, mcpServerFinalizer,
					"expected MCPServer %q to carry the signal-reset finalizer", expectedName)
			}

			// Verify deleted objects are actually gone, not merely left in
			// Terminating. Before mcpServerFinalizer was removed by syncMCPServers
			// itself (§5), a finalizer-bearing stale mirror would never disappear
			// through this fake client, and the old assert.NoError(IgnoreNotFound)
			// check couldn't tell the difference.
			for _, deletedName := range tt.expectDeleted {
				var mcp konnectv1alpha1.MCPServer
				err := cl.Get(t.Context(), client.ObjectKey{Name: deletedName, Namespace: namespace}, &mcp)
				require.True(t, apierrors.IsNotFound(err), "expected MCPServer %q to be fully deleted, got err=%v", deletedName, err)
			}

			// Verify the paired MCPServerDataPlane is created alongside every
			// newly mirrored MCPServer, with the owner reference and
			// spec.mcpServerRef pointing back at it.
			for _, expectedName := range tt.expectDataPlanes {
				var dp mcpv1alpha1.MCPServerDataPlane
				require.NoError(t,
					cl.Get(t.Context(), client.ObjectKey{Name: expectedName, Namespace: namespace}, &dp),
					"expected MCPServerDataPlane %q to exist", expectedName,
				)

				require.NotNil(t, dp.Spec.MCPServerRef.KonnectNamespacedRef)
				assert.Equal(t, mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef, dp.Spec.MCPServerRef.Type)
				assert.Equal(t, expectedName, dp.Spec.MCPServerRef.KonnectNamespacedRef.Name)

				require.Len(t, dp.OwnerReferences, 1)
				assert.Equal(t, expectedName, dp.OwnerReferences[0].Name)
				assert.Equal(t, "MCPServer", dp.OwnerReferences[0].Kind)
				require.NotNil(t, dp.OwnerReferences[0].Controller)
				assert.True(t, *dp.OwnerReferences[0].Controller)
			}

			// No MCPServerDataPlane beyond the expected ones: catches DataPlanes
			// created for skipped/existing servers, or written to the wrong namespace.
			var dpList mcpv1alpha1.MCPServerDataPlaneList
			require.NoError(t, cl.List(t.Context(), &dpList, client.InNamespace(namespace)))
			assert.Len(t, dpList.Items, len(tt.expectDataPlanes))
		})
	}
}
