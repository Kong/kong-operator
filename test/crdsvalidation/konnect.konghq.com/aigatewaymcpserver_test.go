package crdsvalidation

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// validAIGatewayMCPServer returns a minimal, valid AIGatewayMCPServer using the
// "listener" deployment mode, which has the shallowest required-field tree of
// the five discriminated union variants (no required nested Config fields).
func validAIGatewayMCPServer(ns string) *konnectv1alpha1.AIGatewayMCPServer {
	return &konnectv1alpha1.AIGatewayMCPServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AIGatewayMCPServer",
			APIVersion: konnectv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.AIGatewayMCPServerSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "test-ai-gateway",
				},
			},
			APISpec: konnectv1alpha1.AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &konnectv1alpha1.AIGatewayMCPServerConfig{
					Type: konnectv1alpha1.AIGatewayMCPServerConfigTypeListener,
					Listener: &konnectv1alpha1.AIGatewayMCPServerListener{
						Name:        "test-mcp-server",
						DisplayName: "Test MCP Server",
						Access: &konnectv1alpha1.AIGatewayMCPServerListenerAccess{
							AclAttributeType: konnectv1alpha1.AIGatewayMCPServerListenerAccessTypeConsumer,
							Consumer:         &konnectv1alpha1.AIGatewayMCPServerBaseACLPropertiesConsumer{},
						},
						Config: konnectv1alpha1.AIGatewayMCPServerNoUpstreamConfig{
							Route: konnectv1alpha1.AIGatewayRouteConfig{
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
		common.TestCasesGroup[*konnectv1alpha1.AIGatewayMCPServer]{
			{
				Name:       "namespacedRef is accepted",
				TestObject: validAIGatewayMCPServer(ns.Name),
			},
			{
				Name: "type namespacedRef without namespacedRef set is rejected",
				TestObject: func() *konnectv1alpha1.AIGatewayMCPServer {
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
				TestObject: func() *konnectv1alpha1.AIGatewayMCPServer {
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
		common.TestCasesGroup[*konnectv1alpha1.AIGatewayMCPServer]{
			{
				Name: "invalid type value is rejected",
				TestObject: func() *konnectv1alpha1.AIGatewayMCPServer {
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
		common.TestCasesGroup[*konnectv1alpha1.AIGatewayMCPServer]{
			{
				Name: "missing listener.name is rejected",
				TestObject: func() *konnectv1alpha1.AIGatewayMCPServer {
					obj := validAIGatewayMCPServer(ns.Name)
					obj.Spec.APISpec.Listener.Name = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.listener.name"),
			},
			{
				Name: "missing listener.displayName is rejected",
				TestObject: func() *konnectv1alpha1.AIGatewayMCPServer {
					obj := validAIGatewayMCPServer(ns.Name)
					obj.Spec.APISpec.Listener.DisplayName = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.listener.displayName"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}

func validAIGatewayMCPServerConversionOnly(ns string) *konnectv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &konnectv1alpha1.AIGatewayMCPServerConfig{
		Type: konnectv1alpha1.AIGatewayMCPServerConfigTypeConversionOnly,
		ConversionOnly: &konnectv1alpha1.AIGatewayMCPServerConversionOnly{
			Name:        "test-mcp-server-conversion-only",
			DisplayName: "Test MCP Server Conversion Only",
			Config: konnectv1alpha1.AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig{
				URL: "https://example.com/mcp",
			},
		},
	}
	return obj
}

func validAIGatewayMCPServerConversionListener(ns string) *konnectv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &konnectv1alpha1.AIGatewayMCPServerConfig{
		Type: konnectv1alpha1.AIGatewayMCPServerConfigTypeConversionListener,
		ConversionListener: &konnectv1alpha1.AIGatewayMCPServerConversionListener{
			Name:        "test-mcp-server-conversion-listener",
			DisplayName: "Test MCP Server Conversion Listener",
			Access: &konnectv1alpha1.AIGatewayMCPServerConversionListenerAccess{
				AclAttributeType: konnectv1alpha1.AIGatewayMCPServerConversionListenerAccessTypeConsumer,
				Consumer:         &konnectv1alpha1.AIGatewayMCPServerBaseACLPropertiesConsumer{},
			},
			Config: konnectv1alpha1.AIGatewayMCPServerWithUpstreamNoProxyConfig{
				URL: "https://example.com/mcp",
			},
		},
	}
	return obj
}

func validAIGatewayMCPServerPassthroughListener(ns string) *konnectv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &konnectv1alpha1.AIGatewayMCPServerConfig{
		Type: konnectv1alpha1.AIGatewayMCPServerConfigTypePassthroughListener,
		PassthroughListener: &konnectv1alpha1.AIGatewayMCPServerPassthroughListener{
			Name:        "test-mcp-server-passthrough-listener",
			DisplayName: "Test MCP Server Passthrough Listener",
			Access: &konnectv1alpha1.AIGatewayMCPServerPassthroughListenerAccess{
				AclAttributeType: konnectv1alpha1.AIGatewayMCPServerPassthroughListenerAccessTypeConsumer,
				Consumer:         &konnectv1alpha1.AIGatewayMCPServerBaseACLPropertiesConsumer{},
			},
			Config: konnectv1alpha1.AIGatewayMCPServerWithUpstreamConfig{
				URL: "https://example.com/mcp",
			},
		},
	}
	return obj
}

func validAIGatewayMCPServerUpstreamServer(ns string) *konnectv1alpha1.AIGatewayMCPServer {
	obj := validAIGatewayMCPServer(ns)
	obj.Spec.APISpec.AIGatewayMCPServerConfig = &konnectv1alpha1.AIGatewayMCPServerConfig{
		Type: konnectv1alpha1.AIGatewayMCPServerConfigTypeUpstreamServer,
		UpstreamServer: &konnectv1alpha1.AIGatewayMCPServerUpstreamServer{
			Name:        "test-mcp-server-upstream-server",
			DisplayName: "Test MCP Server Upstream Server",
			Access: &konnectv1alpha1.AIGatewayMCPServerUpstreamServerAccess{
				AclAttributeType: konnectv1alpha1.AIGatewayMCPServerUpstreamServerAccessTypeConsumer,
				Consumer:         &konnectv1alpha1.AIGatewayMCPServerBaseACLPropertiesConsumer{},
			},
			Config: konnectv1alpha1.AIGatewayMCPServerUpstreamServerConfig{
				URL:                  "https://example.com/mcp",
				ToolsCacheTtlSeconds: 60,
			},
		},
	}
	return obj
}
