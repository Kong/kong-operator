package crdsvalidation

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// validAIGatewayMCPServer returns a minimal, valid AIGatewayMCPServer using the
// "listener" deployment mode, which has the shallowest required-field tree of
// the five discriminated union variants (no required nested Config fields).
func validAIGatewayMCPServer(ns string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	return &aiconfigurationv1alpha1.AIGatewayMCPServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AIGatewayMCPServer",
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: aiconfigurationv1alpha1.AIGatewayMCPServerSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "test-ai-gateway",
				},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
					Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypeListener,
					Listener: &aiconfigurationv1alpha1.AIGatewayMCPServerListener{
						Name:        "test-mcp-server",
						DisplayName: "Test MCP Server",
						Sources:     []aiconfigurationv1alpha1.AIGatewayEntityIdentifier{"test-source"},
						Access: &aiconfigurationv1alpha1.AIGatewayMCPServerListenerAccess{
							AclAttributeType: aiconfigurationv1alpha1.AIGatewayMCPServerListenerAccessTypeConsumer,
							Consumer:         &aiconfigurationv1alpha1.AIGatewayMCPServerListenerConsumer{},
						},
						Config: aiconfigurationv1alpha1.AIGatewayMCPServerNoUpstreamConfig{
							Route: aiconfigurationv1alpha1.AIGatewayRouteConfig{
								Paths: []string{"/mcp"},
							},
						},
					},
				},
			},
		},
	}
}

func TestAIGatewayMCPServer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("aiGatewayRef validation", func(t *testing.T) {
		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayMCPServer]{
			{
				Name:       "namespacedRef is accepted",
				TestObject: validAIGatewayMCPServer(ns.Name),
			},
			{
				Name: "type namespacedRef without namespacedRef set is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayMCPServer {
					obj := validAIGatewayMCPServer(ns.Name)
					obj.Spec.AIGatewayRef = commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					}
					return obj
				}(),
				ExpectedErrorMessage: new("when type is namespacedRef, namespacedRef must be set"),
			},
			{
				Name: "type konnectID with namespacedRef set is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayMCPServer {
					obj := validAIGatewayMCPServer(ns.Name)
					obj.Spec.AIGatewayRef = commonv1alpha1.ObjectRef{
						Type:      commonv1alpha1.ObjectRefTypeKonnectID,
						KonnectID: new("12345678-1234-1234-1234-123456789abc"),
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "test-ai-gateway",
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("when type is konnectID, namespacedRef must not be set"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("apiSpec.type discriminator validation", func(t *testing.T) {
		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayMCPServer]{
			{
				Name: "invalid type value is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayMCPServer {
					obj := validAIGatewayMCPServer(ns.Name)
					obj.Spec.APISpec.Type = "not-a-real-type"
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.type"),
			},
			{
				Name:       "conversion-only is accepted",
				TestObject: validAIGatewayMCPServerConversionOnly(ns.Name),
			},
			{
				Name:       "conversion-listener is accepted",
				TestObject: validAIGatewayMCPServerConversionListener(ns.Name),
			},
			{
				Name:       "listener is accepted",
				TestObject: validAIGatewayMCPServer(ns.Name),
			},
			{
				Name:       "passthrough-listener is accepted",
				TestObject: validAIGatewayMCPServerPassthroughListener(ns.Name),
			},
			{
				Name:       "upstream-server is accepted",
				TestObject: validAIGatewayMCPServerUpstreamServer(ns.Name),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("required field validation", func(t *testing.T) {
		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayMCPServer]{
			{
				Name: "missing listener.name is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayMCPServer {
					obj := validAIGatewayMCPServer(ns.Name)
					obj.Spec.APISpec.Listener.Name = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.listener.name"),
			},
			{
				Name: "missing listener.displayName is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayMCPServer {
					obj := validAIGatewayMCPServer(ns.Name)
					obj.Spec.APISpec.Listener.DisplayName = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.listener.displayName"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}

func validAIGatewayMCPServerConversionOnly(ns string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
		Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypeConversionOnly,
		ConversionOnly: &aiconfigurationv1alpha1.AIGatewayMCPServerConversionOnly{
			Name:        "test-mcp-server-conversion-only",
			DisplayName: "Test MCP Server Conversion Only",
			Config: aiconfigurationv1alpha1.AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig{
				URL: "https://example.com/mcp",
			},
		},
	}
	return obj
}

func validAIGatewayMCPServerConversionListener(ns string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
		Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypeConversionListener,
		ConversionListener: &aiconfigurationv1alpha1.AIGatewayMCPServerConversionListener{
			Name:        "test-mcp-server-conversion-listener",
			DisplayName: "Test MCP Server Conversion Listener",
			Access: &aiconfigurationv1alpha1.AIGatewayMCPServerConversionListenerAccess{
				AclAttributeType: aiconfigurationv1alpha1.AIGatewayMCPServerConversionListenerAccessTypeConsumer,
				Consumer:         &aiconfigurationv1alpha1.AIGatewayMCPServerListenerConsumer{},
			},
			Config: aiconfigurationv1alpha1.AIGatewayMCPServerWithUpstreamNoProxyConfig{
				URL: "https://example.com/mcp",
			},
		},
	}
	return obj
}

func validAIGatewayMCPServerPassthroughListener(ns string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
		Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypePassthroughListener,
		PassthroughListener: &aiconfigurationv1alpha1.AIGatewayMCPServerPassthroughListener{
			Name:        "test-mcp-server-passthrough-listener",
			DisplayName: "Test MCP Server Passthrough Listener",
			Access: &aiconfigurationv1alpha1.AIGatewayMCPServerPassthroughListenerAccess{
				AclAttributeType: aiconfigurationv1alpha1.AIGatewayMCPServerPassthroughListenerAccessTypeConsumer,
				Consumer:         &aiconfigurationv1alpha1.AIGatewayMCPServerListenerConsumer{},
			},
			Config: aiconfigurationv1alpha1.AIGatewayMCPServerWithUpstreamConfig{
				URL: "https://example.com/mcp",
			},
		},
	}
	return obj
}

func validAIGatewayMCPServerUpstreamServer(ns string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
		Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypeUpstreamServer,
		UpstreamServer: &aiconfigurationv1alpha1.AIGatewayMCPServerUpstreamServer{
			Name:        "test-mcp-server-upstream-server",
			DisplayName: "Test MCP Server Upstream Server",
			Access: &aiconfigurationv1alpha1.AIGatewayMCPServerUpstreamServerAccess{
				AclAttributeType: aiconfigurationv1alpha1.AIGatewayMCPServerUpstreamServerAccessTypeConsumer,
				Consumer:         &aiconfigurationv1alpha1.AIGatewayMCPServerBaseACLPropertiesConsumer{},
			},
			Config: aiconfigurationv1alpha1.AIGatewayMCPServerUpstreamServerConfig{
				URL:                  "https://example.com/mcp",
				ToolsCacheTtlSeconds: 60,
			},
		},
	}
	return obj
}
