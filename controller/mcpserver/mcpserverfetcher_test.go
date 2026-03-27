package mcpserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestSyncMCPServers(t *testing.T) {
	const (
		cpName    = "test-cp"
		cpID      = "cp-konnect-id"
		namespace = "default"
	)

	// controlPlane is the owner KonnectGatewayControlPlane used in all tests.
	controlPlane := &konnectv1alpha1.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cpName,
			Namespace: namespace,
		},
		Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: cpID,
			},
		},
	}

	newServer := func(id, name string) sdkkonnectcomp.MCPServerCPInfo {
		return sdkkonnectcomp.MCPServerCPInfo{
			ID:        id,
			Name:      name,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	// existingMCPServer returns an MCPServer that matches what syncMCPServers would
	// check for (name = cpName-id, mirror ID = id).
	existingMCPServer := func(serverID string) *konnectv1alpha1.MCPServer {
		return &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", cpName, serverID),
				Namespace: namespace,
				Labels: map[string]string{
					labelControlPlaneID: cpID,
				},
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
			expectCreated: []string{fmt.Sprintf("%s-%s", cpName, "srv-name")},
		},
		{
			name:            "existing server (by ID) is skipped without re-creating",
			servers:         []sdkkonnectcomp.MCPServerCPInfo{newServer("srv-id", "srv-id")},
			existingObjects: []client.Object{existingMCPServer("srv-id")},
			expectCreated:   []string{fmt.Sprintf("%s-%s", cpName, "srv-id")},
		},
		{
			name: "stale MCPServer not in Konnect response is deleted",
			servers: []sdkkonnectcomp.MCPServerCPInfo{
				newServer("live-id", "live-id"),
			},
			existingObjects: []client.Object{
				existingMCPServer("live-id"),
				existingMCPServer("stale-id"),
			},
			expectCreated: []string{fmt.Sprintf("%s-%s", cpName, "live-id")},
			expectDeleted: []string{fmt.Sprintf("%s-%s", cpName, "stale-id")},
		},
		{
			name: "mixed: creates new, keeps existing, deletes stale",
			servers: []sdkkonnectcomp.MCPServerCPInfo{
				newServer("existing-id", "existing-id"),
				newServer("new-id", "new-name"),
			},
			existingObjects: []client.Object{
				existingMCPServer("existing-id"),
				existingMCPServer("stale-id"),
			},
			expectCreated: []string{
				fmt.Sprintf("%s-%s", cpName, "existing-id"),
				fmt.Sprintf("%s-%s", cpName, "new-name"),
			},
			expectDeleted: []string{fmt.Sprintf("%s-%s", cpName, "stale-id")},
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
				WithObjects(controlPlane)

			if len(tt.existingObjects) > 0 {
				builder = builder.WithObjects(tt.existingObjects...)
			}
			if tt.interceptFuncs != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptFuncs)
			}
			cl := builder.Build()

			f := NewMCPServersFetcher(logging.DevelopmentMode, cl, nil, make(chan struct{}, 1), controlPlane, s)

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
				assert.Equal(t, cpID, mcp.Labels[labelControlPlaneID])
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
