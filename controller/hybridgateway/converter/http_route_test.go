package converter

import (
	"context"
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	gatewayoperatorv1alpha1 "github.com/kong/kong-operator/api/gateway-operator/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestHostnamesIntersection(t *testing.T) {

	tests := []struct {
		name           string
		route          *gwtypes.HTTPRoute
		objects        []client.Object
		expectedOutput []*configurationv1alpha1.KongRoute
	}{
		{
			name:  "listener with no hostname (accepts all) and route with no hostnames (accepts all)",
			route: newHTTPRouteWithHostnames(),
			objects: append([]client.Object{
				newGatewayWithListenerHostnames(),
			}, newKonnectGatewayStandardObjects()...),
			expectedOutput: newExpectedKongRoutesWithHostnames(map[string][]string{
				"route.1": nil,
			}),
		},
		{
			name:  "single listener and route with specific hostname",
			route: newHTTPRouteWithHostnames("api.example.com"),
			objects: append([]client.Object{
				newGatewayWithListenerHostnames("api.example.com"),
			}, newKonnectGatewayStandardObjects()...),
			expectedOutput: newExpectedKongRoutesWithHostnames(map[string][]string{
				"route.1": {"api.example.com"},
			}),
		},
		{
			name:  "single listener and route with wildcard hostname",
			route: newHTTPRouteWithHostnames("*.example.com"),
			objects: append([]client.Object{
				newGatewayWithListenerHostnames("*.example.com"),
			}, newKonnectGatewayStandardObjects()...),
			expectedOutput: newExpectedKongRoutesWithHostnames(map[string][]string{
				"route.1": {"*.example.com"},
			}),
		},
		{
			name:  "single listener with wildcard hostname matching two hostnames from the route",
			route: newHTTPRouteWithHostnames("api.example.com", "web.example.com"),
			objects: append([]client.Object{
				newGatewayWithListenerHostnames("*.example.com"),
			}, newKonnectGatewayStandardObjects()...),
			expectedOutput: newExpectedKongRoutesWithHostnames(map[string][]string{
				"route.1": {"api.example.com", "web.example.com"},
			}),
		},
		{
			name:  "single listener with wildcard hostname matching only one hostname from the route",
			route: newHTTPRouteWithHostnames("api.example.test", "web.example.com"),
			objects: append([]client.Object{
				newGatewayWithListenerHostnames("*.example.com"),
			}, newKonnectGatewayStandardObjects()...),
			expectedOutput: newExpectedKongRoutesWithHostnames(map[string][]string{
				"route.1": {"web.example.com"},
			}),
		},
		{
			name:  "no matching hostnames between listener and route",
			route: newHTTPRouteWithHostnames("api.example.com"),
			objects: append([]client.Object{
				newGatewayWithListenerHostnames("web.example.com"),
			}, newKonnectGatewayStandardObjects()...),
			expectedOutput: []*configurationv1alpha1.KongRoute{},
		},
		{
			name:  "listener with no hostname (accepts all)",
			route: newHTTPRouteWithHostnames("api.example.com", "web.example.com"),
			objects: append([]client.Object{
				newGatewayWithListenerHostnames(),
			}, newKonnectGatewayStandardObjects()...),
			expectedOutput: newExpectedKongRoutesWithHostnames(map[string][]string{
				"route.1": {"api.example.com", "web.example.com"},
			}),
		},
	}

	scheme := runtime.NewScheme()
	err := gatewayv1.Install(scheme)
	require.NoError(t, err)
	err = configurationv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = konnectv1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = gatewayoperatorv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = operatorv2beta1.AddToScheme(scheme)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			converter := newHTTPRouteConverter(tt.route, fakeClient, true)
			err := converter.Translate()
			require.NoError(t, err)

			output := converter.GetOutputStore(context.TODO())

			// Extract KongRoute objects from the output
			var kongRoutes []*configurationv1alpha1.KongRoute
			for _, obj := range output {
				kongroute := &configurationv1alpha1.KongRoute{}
				err := runtime.DefaultUnstructuredConverter.FromUnstructuredWithValidation(obj.Object, kongroute, true)
				if err != nil || kongroute.Kind != "KongRoute" {
					continue
				}
				kongRoutes = append(kongRoutes, kongroute)
			}

			require.Equal(t, len(tt.expectedOutput), len(kongRoutes), "KongRoute objects number different than expected")

			for _, expectedRoute := range tt.expectedOutput {
				for _, actualRoute := range kongRoutes {
					assert.Equal(t, len(expectedRoute.Spec.Hosts), len(actualRoute.Spec.Hosts), "KongRoute hosts length does not match the expected one")
					for _, h := range expectedRoute.Spec.Hosts {
						assert.Contains(t, actualRoute.Spec.Hosts, h, "KongRoute hosts does not contain expected hostname %s", h)
					}
				}
			}
		})
	}
}

func newHTTPRouteWithHostnames(hostnames ...string) *gwtypes.HTTPRoute {
	var gwHostnames []gatewayv1.Hostname
	for _, h := range hostnames {
		gwHostnames = append(gwHostnames, gatewayv1.Hostname(h))
	}
	return &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Hostnames: gwHostnames,
			Rules: []gwtypes.HTTPRouteRule{
				{
					Matches: []gwtypes.HTTPRouteMatch{
						{
							Path: lo.ToPtr(gatewayv1.HTTPPathMatch{
								Type:  lo.ToPtr(gatewayv1.PathMatchExact),
								Value: lo.ToPtr("/"),
							}),
						},
					},
					BackendRefs: []gwtypes.HTTPBackendRef{
						{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name: "test-service",
									Port: lo.ToPtr(gwtypes.PortNumber(80)),
								},
							},
						},
					},
				},
			},
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{
					{
						Name:  "test-gateway",
						Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
						Group: lo.ToPtr(gwtypes.Group("gateway.networking.k8s.io")),
					},
				},
			},
		},
	}
}

func newGatewayWithListenerHostnames(hostnames ...string) *gwtypes.Gateway {
	var gwListeeners []gwtypes.Listener
	for i, h := range hostnames {
		gwListeeners = append(gwListeeners, gwtypes.Listener{
			Name:     gwtypes.SectionName(fmt.Sprintf("listener-%d", i)),
			Hostname: lo.ToPtr(gatewayv1.Hostname(h)),
		})
	}
	if len(gwListeeners) == 0 {
		// Add a listener with no hostname (accepts all hostnames)
		gwListeeners = append(gwListeeners, gwtypes.Listener{
			Name: "listener-0",
		})
	}

	return &gwtypes.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "test-gateway-class",
			Listeners:        gwListeeners,
		},
	}
}

func newKonnectGatewayStandardObjects() []client.Object {
	objects := []client.Object{
		&gwtypes.GatewayClass{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GatewayClass",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-gateway-class",
			},
			Spec: gwtypes.GatewayClassSpec{
				ControllerName: "konghq.com/gateway-controller",
				ParametersRef: &gwtypes.ParametersReference{
					Group:     "gateway-operator.konghq.com",
					Kind:      gwtypes.Kind("GatewayConfiguration"),
					Name:      "test-gateway-config",
					Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
				},
			},
		},
		&gwtypes.GatewayConfiguration{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GatewayConfiguration",
				APIVersion: "gateway-operator.konghq.com/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-gateway-config",
				Namespace: "default",
			},
			Spec: gwtypes.GatewayConfigurationSpec{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: "konnect.konghq.com",
						Kind:  "KonnectExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name:      "test-extension",
							Namespace: lo.ToPtr("default"),
						},
					},
				},
			},
		},
		&konnectv1alpha2.KonnectExtension{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KonnectExtension",
				APIVersion: "konnect.konghq.com/v1alpha2",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-extension",
				Namespace: "default",
			},
			Spec: konnectv1alpha2.KonnectExtensionSpec{
				Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
					ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
						Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
							Type: "konnectNamespacedRef",
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name:      "default",
								Namespace: "default",
							},
						},
					},
				},
			},
		},
		&konnectv1alpha2.KonnectGatewayControlPlane{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KonnectControlPlane",
				APIVersion: "konnect.konghq.com/v1alpha2",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: "default",
			},
			Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
				CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
					Name: "default",
				},
			},
		}}
	return objects
}

func newExpectedKongRoutesWithHostnames(routeHostnames map[string][]string) []*configurationv1alpha1.KongRoute {
	var kongRoutes []*configurationv1alpha1.KongRoute
	for routeKey, hostnames := range routeHostnames {
		kongRoute := &configurationv1alpha1.KongRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      routeKey,
				Namespace: "default",
			},
			Spec: configurationv1alpha1.KongRouteSpec{
				KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
					Hosts: hostnames,
				},
			},
		}
		kongRoutes = append(kongRoutes, kongRoute)
	}
	return kongRoutes
}
