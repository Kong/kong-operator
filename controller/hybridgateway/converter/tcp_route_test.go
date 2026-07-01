package converter

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestNewConverterTCPRoute(t *testing.T) {
	route := newTCPRouteForTranslation()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()

	converter, err := NewConverter(*route, fakeClient, false, "")
	require.NoError(t, err)
	_, ok := converter.(*tcpRouteConverter)
	require.True(t, ok)
}

func TestTCPRouteConverter_Translate(t *testing.T) {
	route := newTCPRouteForTranslation()
	gateway := newTCPGateway()
	gateway.UID = types.UID("gateway-uid")
	objects := append(
		newKonnectGatewayStandardObjects(gateway),
		newService("default"),
		newEndpointSlice("backend-service", "default", []string{"10.0.1.1", "10.0.1.2"}),
	)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
	converter := newTCPRouteConverter(route, fakeClient, false, "")

	resourceCount, err := converter.Translate(t.Context(), logr.Discard())
	require.NoError(t, err)
	require.Equal(t, 5, resourceCount)

	output, err := converter.GetOutputStore(t.Context(), logr.Discard())
	require.NoError(t, err)
	require.Len(t, output, 5)

	kindCounts := map[string]int{}
	var kongRoute *configurationv1alpha1.KongRoute
	for _, obj := range output {
		kindCounts[obj.GetKind()]++
		if obj.GetKind() == "KongRoute" {
			converted := &configurationv1alpha1.KongRoute{}
			require.NoError(t, fakeClient.Scheme().Convert(&obj, converted, nil))
			kongRoute = converted
		}
	}

	assert.Equal(t, 1, kindCounts["KongUpstream"])
	assert.Equal(t, 1, kindCounts["KongService"])
	assert.Equal(t, 1, kindCounts["KongRoute"])
	assert.Equal(t, 2, kindCounts["KongTarget"])
	require.NotNil(t, kongRoute)
	assert.ElementsMatch(t, []sdkkonnectcomp.Protocols{sdkkonnectcomp.ProtocolsTCP}, kongRoute.Spec.Protocols)
	assert.Empty(t, kongRoute.Spec.Hosts)
	assert.Empty(t, kongRoute.Spec.Paths)
	assert.Equal(t, "default/test-route", kongRoute.Annotations[consts.GatewayOperatorHybridRoutesTCPRouteAnnotation])
}

func TestTCPRouteConverter_GetHybridGatewayParentsIsHostless(t *testing.T) {
	route := newTCPRouteForTranslation()
	gateway := newTCPGateway()
	gateway.Spec.Listeners[0].Hostname = new(gatewayv1.Hostname("listener.example.com"))
	gateway.UID = types.UID("gateway-uid")
	objects := newKonnectGatewayStandardObjects(gateway)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()

	parents, err := getHybridGatewayParents(t.Context(), logr.Discard(), fakeClient, route)
	require.NoError(t, err)
	require.Len(t, parents, 1)
	assert.Empty(t, parents[0].hostnames)
}

func newTCPRouteForTranslation() *gwtypes.TCPRoute {
	port := gwtypes.PortNumber(80)
	return &gwtypes.TCPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "TCPRoute",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gwtypes.TCPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name:  "test-gateway",
					Kind:  new(gwtypes.Kind("Gateway")),
					Group: new(gwtypes.Group(gwtypes.GroupName)),
				}},
			},
			Rules: []gwtypes.TCPRouteRule{{
				BackendRefs: []gwtypes.BackendRef{{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:  "backend-service",
						Port:  &port,
						Kind:  new(gwtypes.Kind("Service")),
						Group: new(gwtypes.Group("")),
					},
				}},
			}},
		},
	}
}

func newTCPGateway() *gwtypes.Gateway {
	gateway := newGatewayWithListenerHostnames()
	gateway.Spec.Listeners[0].Protocol = gatewayv1.TCPProtocolType
	gateway.Status.Listeners[0].SupportedKinds = []gatewayv1.RouteGroupKind{{
		Group: new(gatewayv1.Group(gatewayv1.GroupName)),
		Kind:  "TCPRoute",
	}}
	return gateway
}
