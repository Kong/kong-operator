package converter

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestNewConverterUDPRoute(t *testing.T) {
	route := newUDPRouteForTranslation()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()

	converter, err := NewConverter(*route, fakeClient, false, "")
	require.NoError(t, err)
	_, ok := converter.(*udpRouteConverter)
	require.True(t, ok)
}

func TestUDPRouteConverter_Translate(t *testing.T) {
	route := newUDPRouteForTranslation()
	gateway := newUDPGateway()
	gateway.UID = types.UID("gateway-uid")
	objects := append(
		newKonnectGatewayStandardObjects(gateway),
		newService("default"),
		newEndpointSlice("backend-service", "default", []string{"10.0.1.1", "10.0.1.2"}),
	)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
	converter := newUDPRouteConverter(route, fakeClient, false, "")

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
	assert.ElementsMatch(t, []sdkkonnectcomp.Protocols{sdkkonnectcomp.ProtocolsUDP}, kongRoute.Spec.Protocols)
	assert.Empty(t, kongRoute.Spec.Hosts)
	assert.Empty(t, kongRoute.Spec.Paths)
	assert.Equal(t, "default/test-route", kongRoute.Annotations[consts.GatewayOperatorHybridRoutesUDPRouteAnnotation])
}

func TestUDPRouteConverter_TranslateKeepsOldestRouteForSameListener(t *testing.T) {
	olderRoute := newUDPRouteForTranslation()
	olderRoute.CreationTimestamp = metav1.NewTime(time.Unix(1, 0))
	newerRoute := newUDPRouteForTranslation()
	newerRoute.Name = "newer-route"
	newerRoute.CreationTimestamp = metav1.NewTime(time.Unix(2, 0))

	gateway := newUDPGateway()
	gateway.UID = types.UID("gateway-uid")
	objects := append(
		newKonnectGatewayStandardObjects(gateway),
		newerRoute,
		newService("default"),
		newEndpointSlice("backend-service", "default", []string{"10.0.1.1", "10.0.1.2"}),
	)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
	converter := newUDPRouteConverter(olderRoute, fakeClient, false, "")

	resourceCount, err := converter.Translate(t.Context(), logr.Discard())
	require.NoError(t, err)
	require.Equal(t, 5, resourceCount)

	output, err := converter.GetOutputStore(t.Context(), logr.Discard())
	require.NoError(t, err)

	var kongRoute *configurationv1alpha1.KongRoute
	for _, obj := range output {
		if obj.GetKind() != "KongRoute" {
			continue
		}
		converted := &configurationv1alpha1.KongRoute{}
		require.NoError(t, fakeClient.Scheme().Convert(&obj, converted, nil))
		kongRoute = converted
	}

	require.NotNil(t, kongRoute)
	require.Len(t, kongRoute.Spec.Destinations, 1)
	require.NotNil(t, kongRoute.Spec.Destinations[0].Port)
	assert.Equal(t, int64(80), *kongRoute.Spec.Destinations[0].Port)
}

func TestUDPRouteConverter_TranslateSkipsNewerRouteForSameListener(t *testing.T) {
	olderRoute := newUDPRouteForTranslation()
	olderRoute.Name = "older-route"
	olderRoute.CreationTimestamp = metav1.NewTime(time.Unix(1, 0))
	newerRoute := newUDPRouteForTranslation()
	newerRoute.CreationTimestamp = metav1.NewTime(time.Unix(2, 0))

	gateway := newUDPGateway()
	gateway.UID = types.UID("gateway-uid")
	objects := append(
		newKonnectGatewayStandardObjects(gateway),
		olderRoute,
	)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
	converter := newUDPRouteConverter(newerRoute, fakeClient, false, "")

	resourceCount, err := converter.Translate(t.Context(), logr.Discard())
	require.NoError(t, err)
	assert.Zero(t, resourceCount)

	output, err := converter.GetOutputStore(t.Context(), logr.Discard())
	require.NoError(t, err)
	assert.Empty(t, output)
}

func TestPickWinningL4RouteTieBreaksByNamespaceAndName_UDPRoute(t *testing.T) {
	route := newUDPRouteForTranslation()
	route.Namespace = "b"
	route.Name = "a"
	route.CreationTimestamp = metav1.NewTime(time.Unix(1, 0))
	winnerByNamespace := newUDPRouteForTranslation()
	winnerByNamespace.Namespace = "a"
	winnerByNamespace.Name = "z"
	winnerByNamespace.CreationTimestamp = route.CreationTimestamp
	winnerByName := newUDPRouteForTranslation()
	winnerByName.Namespace = "b"
	winnerByName.Name = "0"
	winnerByName.CreationTimestamp = route.CreationTimestamp

	winner, ok := pickWinningL4Route([]*gwtypes.UDPRoute{route, winnerByNamespace})
	require.True(t, ok)
	require.Same(t, winnerByNamespace, winner)

	winner, ok = pickWinningL4Route([]*gwtypes.UDPRoute{route, winnerByName})
	require.True(t, ok)
	require.Same(t, winnerByName, winner)
}

func TestUDPRouteConverter_TranslateBackendClientCertificate(t *testing.T) {
	route := newUDPRouteForTranslation()
	gateway := newUDPGateway()
	gateway.UID = types.UID("gateway-uid")

	backendService := newService("default")
	backendService.Annotations = map[string]string{
		"konghq.com/client-cert": "backend-client-cert",
		"konghq.com/protocol":    "tls",
	}
	clientCertSecret := &corev1.Secret{
		Name:      "backend-client-cert",
		Namespace: "default",
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte("cert-data"),
			corev1.TLSPrivateKeyKey: []byte("key-data"),
		},
	}
	objects := append(
		newKonnectGatewayStandardObjects(gateway),
		backendService,
		clientCertSecret,
		newEndpointSlice("backend-service", "default", []string{"10.0.1.1", "10.0.1.2"}),
	)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
	converter := newUDPRouteConverter(route, fakeClient, false, "")

	resourceCount, err := converter.Translate(t.Context(), logr.Discard())
	require.NoError(t, err)
	require.Equal(t, 6, resourceCount)

	output, err := converter.GetOutputStore(t.Context(), logr.Discard())
	require.NoError(t, err)

	var kongService *configurationv1alpha1.KongService
	var kongCertificate *configurationv1alpha1.KongCertificate
	for _, obj := range output {
		switch obj.GetKind() {
		case "KongService":
			converted := &configurationv1alpha1.KongService{}
			require.NoError(t, fakeClient.Scheme().Convert(&obj, converted, nil))
			kongService = converted
		case "KongCertificate":
			converted := &configurationv1alpha1.KongCertificate{}
			require.NoError(t, fakeClient.Scheme().Convert(&obj, converted, nil))
			kongCertificate = converted
		}
	}

	require.NotNil(t, kongService)
	require.NotNil(t, kongCertificate)
	require.NotNil(t, kongService.Spec.ClientCertificateRef)
	assert.Equal(t, kongCertificate.Name, kongService.Spec.ClientCertificateRef.Name)
	require.NotNil(t, kongCertificate.Spec.SecretRef)
	assert.Equal(t, "backend-client-cert", kongCertificate.Spec.SecretRef.Name)
	require.NotNil(t, kongCertificate.Spec.SecretRef.Namespace)
	assert.Equal(t, "default", *kongCertificate.Spec.SecretRef.Namespace)
	assert.Equal(t, "default/test-route", kongCertificate.Annotations[consts.GatewayOperatorHybridRoutesUDPRouteAnnotation])
}

func TestUDPRouteConverter_GetHybridGatewayParentsIsHostless(t *testing.T) {
	route := newUDPRouteForTranslation()
	gateway := newUDPGateway()
	gateway.Spec.Listeners[0].Hostname = new(gatewayv1.Hostname("listener.example.com"))
	gateway.UID = types.UID("gateway-uid")
	objects := newKonnectGatewayStandardObjects(gateway)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()

	parents, err := getHybridGatewayParents(t.Context(), logr.Discard(), fakeClient, route)
	require.NoError(t, err)
	require.Len(t, parents, 1)
	assert.Empty(t, parents[0].hostnames)
}

// TestUDPRouteConverter_HandleOrphanedResource covers two UDPRoutes that collide on the
// same backend Service+port and therefore share a KongService/KongUpstream: deleting one
// route must strip only its own reference from the hybrid-routes annotation and keep the
// shared resource alive while the other route still references it, and must only allow
// deletion once no UDPRoute reference remains.
func TestUDPRouteConverter_HandleOrphanedResource(t *testing.T) {
	route := newUDPRouteForTranslation()

	tests := []struct {
		name        string
		setup       func() (*udpRouteConverter, *unstructured.Unstructured)
		wantErr     bool
		wantSkip    bool
		errContains string
		assertFn    func(t *testing.T, resource *unstructured.Unstructured)
	}{
		{
			name: "skips resource without route annotation",
			setup: func() (*udpRouteConverter, *unstructured.Unstructured) {
				resource := newUDPUnstructuredResource("")
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
				return newUDPRouteConverter(route, fakeClient, false, "").(*udpRouteConverter), resource
			},
			wantSkip: true,
		},
		{
			name: "updates annotation and keeps shared resource when colliding route remains",
			setup: func() (*udpRouteConverter, *unstructured.Unstructured) {
				routeKey := client.ObjectKeyFromObject(route).String()
				// Simulates a second UDPRoute in the same namespace colliding on the
				// same backend Service+port, so both routes' refs are on the shared resource.
				resource := newUDPUnstructuredResource(routeKey + ",default/other-udproute")
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
				return newUDPRouteConverter(route, fakeClient, false, "").(*udpRouteConverter), resource
			},
			wantSkip: true,
			assertFn: func(t *testing.T, resource *unstructured.Unstructured) {
				assert.Equal(t, "default/other-udproute", resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesUDPRouteAnnotation])
			},
		},
		{
			name: "allows deletion when only route remains",
			setup: func() (*udpRouteConverter, *unstructured.Unstructured) {
				routeKey := client.ObjectKeyFromObject(route).String()
				resource := newUDPUnstructuredResource(routeKey)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
				return newUDPRouteConverter(route, fakeClient, false, "").(*udpRouteConverter), resource
			},
			assertFn: func(t *testing.T, resource *unstructured.Unstructured) {
				_, exists := resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesUDPRouteAnnotation]
				assert.False(t, exists)
			},
		},
		{
			name: "returns error when patch fails",
			setup: func() (*udpRouteConverter, *unstructured.Unstructured) {
				routeKey := client.ObjectKeyFromObject(route).String()
				resource := newUDPUnstructuredResource(routeKey + ",default/other-udproute")
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(resource).
					WithInterceptorFuncs(interceptor.Funcs{
						Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
							return fmt.Errorf("simulated patch error")
						},
					}).
					Build()
				return newUDPRouteConverter(route, fakeClient, false, "").(*udpRouteConverter), resource
			},
			wantErr:     true,
			wantSkip:    true,
			errContains: "failed to update resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter, resource := tt.setup()
			skipDelete, err := converter.HandleOrphanedResource(t.Context(), logr.Discard(), resource)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Equal(t, tt.wantSkip, skipDelete)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSkip, skipDelete)
			if tt.assertFn != nil {
				tt.assertFn(t, resource)
			}
		})
	}
}

func newUDPUnstructuredResource(routesAnnotation string) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongService"))
	resource.SetName("orphaned")
	resource.SetNamespace("default")
	if routesAnnotation != "" {
		resource.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridRoutesUDPRouteAnnotation: routesAnnotation,
		})
	}
	return resource
}

func newUDPRouteForTranslation() *gwtypes.UDPRoute {
	port := gwtypes.PortNumber(80)
	return &gwtypes.UDPRoute{
		Kind:       "UDPRoute",
		APIVersion: "gateway.networking.k8s.io/v1",
		Name:       "test-route",
		Namespace:  "default",
		Spec: gwtypes.UDPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name:  "test-gateway",
					Kind:  new(gwtypes.Kind("Gateway")),
					Group: new(gwtypes.Group(gwtypes.GroupName)),
				}},
			},
			Rules: []gwtypes.UDPRouteRule{{
				BackendRefs: []gwtypes.BackendRef{{
					Name:  "backend-service",
					Port:  &port,
					Kind:  new(gwtypes.Kind("Service")),
					Group: new(gwtypes.Group("")),
				}},
			}},
		},
	}
}

func newUDPGateway() *gwtypes.Gateway {
	gateway := newGatewayWithListenerHostnames()
	gateway.Spec.Listeners[0].Protocol = gatewayv1.UDPProtocolType
	gateway.Status.Listeners[0].SupportedKinds = []gatewayv1.RouteGroupKind{{
		Group: new(gatewayv1.Group(gatewayv1.GroupName)),
		Kind:  "UDPRoute",
	}}
	return gateway
}
