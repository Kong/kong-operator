package crdsvalidation_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayMCPServer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := &konnectv1alpha1.AIGatewayMCPServer{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AIGatewayMCPServer",
				APIVersion: konnectv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.AIGatewayMCPServerSpec{
				AIGatewayRef: commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: konnectv1alpha1.AIGatewayMCPServerAPISpec{
					AIGatewayMCPServerConfig: &konnectv1alpha1.AIGatewayMCPServerConfig{
						Type: konnectv1alpha1.AIGatewayMCPServerConfigTypeListener,
						Listener: &konnectv1alpha1.AIGatewayMCPServerListener{
							Name:        "mcpserver1",
							DisplayName: "Test MCP Server",
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
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj)
	})
}
