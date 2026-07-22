package create

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/test/controllers/gateway"
	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/util/builder"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
)

// PublishServiceName is the name of the publish service used in Gateway API tests.
const PublishServiceName = "publish-svc"

// Gateway deploys a Gateway, GatewayClass and an ingress service for use in tests.
func Gateway(ctx context.Context, t *testing.T, client client.Client) (gatewayapi.Gateway, gatewayapi.GatewayClass) {
	gwc := gatewayapi.GatewayClass{
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: gateway.GetControllerName(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gwc-",
			Annotations: map[string]string{
				"konghq.com/gatewayclass-unmanaged": "placeholder",
			},
		},
	}
	require.NoError(t, client.Create(ctx, &gwc))
	t.Cleanup(func() { _ = client.Delete(ctx, &gwc) })

	gw := GatewayUsingGatewayClass(ctx, t, client, gwc)

	return gw, gwc
}

// Namespace creates namespace using the provided client and returns it.
func Namespace(ctx context.Context, t *testing.T, cl client.Client) *corev1.Namespace {
	t.Helper()

	labelOpts := func(obj client.Object) {
		ns, ok := obj.(*corev1.Namespace)
		if !ok {
			t.Fatalf("expected *corev1.Namespace, got %T", obj)
		}
		ns.Labels = map[string]string{
			"test": "envtest",
		}

	}
	return deploy.Namespace(t, ctx, cl, labelOpts)
}

// GatewayUsingGatewayClass deploys a Gateway, GatewayClass
// and an ingress service for use in tests.
func GatewayUsingGatewayClass(ctx context.Context, t *testing.T, client client.Client, gwc gatewayapi.GatewayClass) gatewayapi.Gateway {
	ns := Namespace(ctx, t, client)

	publishSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      PublishServiceName,
		},
		Spec: corev1.ServiceSpec{
			Ports: builder.NewServicePort().
				WithName("http").
				WithProtocol(corev1.ProtocolTCP).
				WithPort(8000).
				IntoSlice(),
		},
	}
	require.NoError(t, client.Create(ctx, &publishSvc))
	t.Cleanup(func() { _ = client.Delete(ctx, &publishSvc) })

	gw := gatewayapi.Gateway{
		Spec: gatewayapi.GatewaySpec{
			GatewayClassName: gatewayapi.ObjectName(gwc.Name),
			Listeners: []gatewayapi.Listener{
				{
					Name:          "http",
					Protocol:      gatewayapi.HTTPProtocolType,
					Port:          gatewayapi.PortNumber(8000),
					AllowedRoutes: builder.NewAllowedRoutesFromAllNamespaces(),
				},
			},
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    ns.Name,
			GenerateName: "gw-",
		},
	}
	require.NoError(t, client.Create(ctx, &gw))
	t.Cleanup(func() { _ = client.Delete(ctx, &gw) })

	return gw
}
