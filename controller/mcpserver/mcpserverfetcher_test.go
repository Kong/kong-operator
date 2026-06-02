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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      cpName,
			Namespace: namespace,
		},
		Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: cpID,
			},
		},
	}

	resourceID := "resource-id"
	newServer := func(id, name string) sdkkonnectcomp.MCPServerCPInfo {
		return sdkkonnectcomp.MCPServerCPInfo{
			ID:         id,
			Name:       name,
			ResourceID: &resourceID,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	// existingMCPServer returns an MCPServer that matches what syncMCPServers would
	// check for, using generateMCPServerNN for the name and the given mirror ID.
	existingMCPServer := func(serverID string) *konnectv1alpha1.MCPServer {
		nn := generateMCPServerNN(namespace, cpName, serverID)
		return &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
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
		name            string
		servers         []sdkkonnectcomp.MCPServerCPInfo
		existingObjects []client.Object
		interceptFuncs  *interceptor.Funcs
		expectError     bool
		expectCreated   []string // MCPServer names expected to exist after sync
		expectDeleted   []string // MCPServer names expected to be gone after sync
	}{
		{
			name:          "no servers, no existing objects is a no-op",
			servers:       []sdkkonnectcomp.MCPServerCPInfo{},
			expectCreated: []string{},
		},
		{
			name:          "new server is created",
			servers:       []sdkkonnectcomp.MCPServerCPInfo{newServer("srv-id", "srv-name")},
			expectCreated: []string{generateMCPServerNN(namespace, cpName, "srv-id").Name},
		},
		{
			name:            "existing server (by ID) is skipped without re-creating",
			servers:         []sdkkonnectcomp.MCPServerCPInfo{newServer("srv-id", "srv-name")},
			existingObjects: []client.Object{existingMCPServer("srv-id")},
			expectCreated:   []string{generateMCPServerNN(namespace, cpName, "srv-id").Name},
		},
		{
			name: "stale MCPServer not in Konnect response is deleted",
			servers: []sdkkonnectcomp.MCPServerCPInfo{
				newServer("live-id", "live-name"),
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
				newServer("existing-id", "existing-name"),
				newServer("new-id", "new-name"),
			},
			existingObjects: []client.Object{
				existingMCPServer("existing-id"),
				existingMCPServer("stale-id"),
			},
			expectCreated: []string{
				generateMCPServerNN(namespace, cpName, "existing-id").Name,
				generateMCPServerNN(namespace, cpName, "new-id").Name,
			},
			expectDeleted: []string{generateMCPServerNN(namespace, cpName, "stale-id").Name},
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

			err := f.syncMCPServers(context.Background(), tt.servers)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify expected objects exist.
			for _, expectedName := range tt.expectCreated {
				var mcp konnectv1alpha1.MCPServer
				require.NoError(t,
					cl.Get(context.Background(), client.ObjectKey{Name: expectedName, Namespace: namespace}, &mcp),
					"expected MCPServer %q to exist", expectedName,
				)
			}

			// Verify deleted objects are gone.
			for _, deletedName := range tt.expectDeleted {
				var mcp konnectv1alpha1.MCPServer
				err := cl.Get(context.Background(), client.ObjectKey{Name: deletedName, Namespace: namespace}, &mcp)
				assert.NoError(t, client.IgnoreNotFound(err), "expected MCPServer %q to be deleted", deletedName)
			}
		})
	}
}
