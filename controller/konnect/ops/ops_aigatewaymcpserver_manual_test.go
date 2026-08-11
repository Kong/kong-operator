package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestGetAIGatewayMCPServerForUID(t *testing.T) {
	t.Run("matches by kubernetes UID label when present, with an oauth-flavored access policy", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayMCPServersSDK(t)
		obj := testAIGatewayMCPServer()

		sdk.EXPECT().
			ListAiGatewayMcpServers(mock.Anything, sdkkonnectops.ListAiGatewayMcpServersRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayMcpServersResponse{
				ListAIGatewayMCPServersResponse: &sdkkonnectcomp.ListAIGatewayMCPServersResponse{
					Data: []sdkkonnectcomp.AIGatewayMCPServer{
						{
							AIGatewayMCPServerAIGatewayMCPServerListener: &sdkkonnectcomp.AIGatewayMCPServerAIGatewayMCPServerListener{
								ID:     "other-id",
								Name:   "other-mcp-server",
								Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"},
								Access: &sdkkonnectcomp.AIGatewayMCPServerListenerAccess{
									Type:                            sdkkonnectcomp.AIGatewayMCPServerListenerAccessTypeOauthAccessToken,
									AIGatewayMCPServerListenerOauth: &sdkkonnectcomp.AIGatewayMCPServerListenerOauth{},
								},
							},
							Type: sdkkonnectcomp.AIGatewayMCPServerTypeListener,
						},
						{
							AIGatewayMCPServerAIGatewayMCPServerListener: &sdkkonnectcomp.AIGatewayMCPServerAIGatewayMCPServerListener{
								ID:     "matched-by-uid",
								Name:   "different-name",
								Labels: map[string]string{KubernetesUIDLabelKey: string(obj.GetUID())},
								Access: &sdkkonnectcomp.AIGatewayMCPServerListenerAccess{
									Type:                            sdkkonnectcomp.AIGatewayMCPServerListenerAccessTypeOauthAccessToken,
									AIGatewayMCPServerListenerOauth: &sdkkonnectcomp.AIGatewayMCPServerListenerOauth{},
								},
							},
							Type: sdkkonnectcomp.AIGatewayMCPServerTypeListener,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayMCPServerForUID(ctx, sdk, obj)
		require.NoError(t, err)
		assert.Equal(t, "matched-by-uid", id)
	})

	t.Run("falls back to matching by type and name, with a consumer-flavored access policy", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayMCPServersSDK(t)
		obj := testAIGatewayMCPServer()

		sdk.EXPECT().
			ListAiGatewayMcpServers(mock.Anything, sdkkonnectops.ListAiGatewayMcpServersRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayMcpServersResponse{
				ListAIGatewayMCPServersResponse: &sdkkonnectcomp.ListAIGatewayMCPServersResponse{
					Data: []sdkkonnectcomp.AIGatewayMCPServer{
						{
							AIGatewayMCPServerAIGatewayMCPServerConversionOnly: &sdkkonnectcomp.AIGatewayMCPServerAIGatewayMCPServerConversionOnly{
								ID:   "wrong-variant",
								Name: "flights-mcp-server",
							},
							Type: sdkkonnectcomp.AIGatewayMCPServerTypeConversionOnly,
						},
						{
							AIGatewayMCPServerAIGatewayMCPServerListener: &sdkkonnectcomp.AIGatewayMCPServerAIGatewayMCPServerListener{
								ID:   "matched-by-name",
								Name: "flights-mcp-server",
								Access: &sdkkonnectcomp.AIGatewayMCPServerListenerAccess{
									Type:                               sdkkonnectcomp.AIGatewayMCPServerListenerAccessTypeConsumer,
									AIGatewayMCPServerListenerConsumer: &sdkkonnectcomp.AIGatewayMCPServerListenerConsumer{},
								},
							},
							Type: sdkkonnectcomp.AIGatewayMCPServerTypeListener,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayMCPServerForUID(ctx, sdk, obj)
		require.NoError(t, err)
		assert.Equal(t, "matched-by-name", id)
	})

	t.Run("returns not found when no matching entry exists", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayMCPServersSDK(t)
		obj := testAIGatewayMCPServer()

		sdk.EXPECT().
			ListAiGatewayMcpServers(mock.Anything, sdkkonnectops.ListAiGatewayMcpServersRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayMcpServersResponse{
				ListAIGatewayMCPServersResponse: &sdkkonnectcomp.ListAIGatewayMCPServersResponse{
					Data: []sdkkonnectcomp.AIGatewayMCPServer{
						{
							AIGatewayMCPServerAIGatewayMCPServerListener: &sdkkonnectcomp.AIGatewayMCPServerAIGatewayMCPServerListener{
								ID:   "other-id",
								Name: "other-mcp-server",
								Access: &sdkkonnectcomp.AIGatewayMCPServerListenerAccess{
									Type:                               sdkkonnectcomp.AIGatewayMCPServerListenerAccessTypeConsumer,
									AIGatewayMCPServerListenerConsumer: &sdkkonnectcomp.AIGatewayMCPServerListenerConsumer{},
								},
							},
							Type: sdkkonnectcomp.AIGatewayMCPServerTypeListener,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayMCPServerForUID(ctx, sdk, obj)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})
}

func testAIGatewayMCPServer() *konnectv1alpha1.AIGatewayMCPServer {
	return &konnectv1alpha1.AIGatewayMCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "AIGatewayMCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "aigatewaymcpserver",
			Namespace:  "default",
			UID:        "aigatewaymcpserver-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.AIGatewayMCPServerSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "ai-gw-cp-1",
				},
			},
			APISpec: konnectv1alpha1.AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &konnectv1alpha1.AIGatewayMCPServerConfig{
					Type: konnectv1alpha1.AIGatewayMCPServerConfigTypeListener,
					Listener: &konnectv1alpha1.AIGatewayMCPServerListener{
						Name: "flights-mcp-server",
					},
				},
			},
		},
		Status: konnectv1alpha1.AIGatewayMCPServerStatus{
			GatewayID: &konnectv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
		},
	}
}
