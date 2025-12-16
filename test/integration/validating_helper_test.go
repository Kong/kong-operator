package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
)

func bootstrapGateway(ctx context.Context, t *testing.T, env environments.Environment, mgrClient client.Client) (
	namespace *v1.Namespace, cleaner *clusters.Cleaner, ingressClass string, ctrlClient client.Client,
) {
	namespace, cleaner = helpers.SetupTestEnv(t, ctx, env)

	ctrlClient = client.NewNamespacedClient(mgrClient, namespace.Name)

	ingressClass = envconf.RandomName("ingressclass", 16)

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name, func(gc *operatorv2beta1.GatewayConfiguration) {
		gc.Spec.ControlPlaneOptions = &operatorv2beta1.GatewayConfigControlPlaneOptions{
			ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
				IngressClass: lo.ToPtr(ingressClass),
			},
		}
	})
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	require.NoError(t, ctrlClient.Create(ctx, gatewayConfig))
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	require.NoError(t, ctrlClient.Create(ctx, gatewayClass))
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass)
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	require.NoError(t, ctrlClient.Create(ctx, gateway))
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients), 3*time.Minute, time.Second)
	t.Log("Gateway is programmed, proceeding with the test cases")

	return namespace, cleaner, ingressClass, ctrlClient
}
