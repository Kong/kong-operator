package converter

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

func TestHTTPRouteConverter_GetOutputStore(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	validUpstream := &configurationv1alpha1.KongUpstream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "upstream-1",
			Namespace: "default",
		},
	}
	validService := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service-1",
			Namespace: "default",
		},
	}

	tests := []struct {
		name          string
		outputStore   []client.Object
		wantErr       bool
		wantLen       int
		wantNames     []string
		wantErrSubstr []string
	}{
		{
			name:        "all objects convert successfully",
			outputStore: []client.Object{validUpstream, validService},
			wantLen:     2,
			wantNames:   []string{"upstream-1", "service-1"},
		},
		{
			name:          "one object fails conversion",
			outputStore:   []client.Object{validUpstream, &badObject{Name: "bad-1"}, validService},
			wantErr:       true,
			wantLen:       2,
			wantErrSubstr: []string{"output store conversion failed with 1 errors", "failed to convert *converter.badObject bad-1 to unstructured"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
			converter := newHTTPRouteConverter(&gwtypes.HTTPRoute{}, fakeClient, false, "").(*httpRouteConverter)
			converter.outputStore = tt.outputStore

			objects, err := converter.GetOutputStore(ctx, logger)
			if tt.wantErr {
				require.Error(t, err)
				for _, substr := range tt.wantErrSubstr {
					assert.Contains(t, err.Error(), substr)
				}
			} else {
				require.NoError(t, err)
			}
			require.Len(t, objects, tt.wantLen)
			for i, name := range tt.wantNames {
				assert.Equal(t, name, objects[i].GetName())
			}
		})
	}
}

func TestHTTPRouteConverter_TranslateDeduplicatesSharedBackendResources(t *testing.T) {
	backendRef := newBackendRef("")
	route := newHTTPRouteWithRules(nil, []gwtypes.HTTPRouteRule{
		{
			Matches: []gwtypes.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  new(gatewayv1.PathMatchExact),
					Value: new("/one"),
				},
			}},
			BackendRefs: []gwtypes.HTTPBackendRef{backendRef},
		},
		{
			Matches: []gwtypes.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  new(gatewayv1.PathMatchPathPrefix),
					Value: new("/two"),
				},
			}},
			BackendRefs: []gwtypes.HTTPBackendRef{backendRef},
		},
	})

	gateway := newGatewayWithListenerHostnames()
	gateway.UID = types.UID("gateway-uid")
	objects := append(
		newKonnectGatewayStandardObjects(gateway),
		newService("default"),
		newEndpointSlice("backend-service", "default", []string{"10.0.1.1", "10.0.1.2"}),
	)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()

	converter := newHTTPRouteConverter(route, fakeClient, false, "")
	resourceCount, err := converter.Translate(t.Context(), logr.Discard())
	require.NoError(t, err)
	require.Equal(t, 6, resourceCount)

	output, err := converter.GetOutputStore(t.Context(), logr.Discard())
	require.NoError(t, err)
	require.Len(t, output, 6)

	kindCounts := map[string]int{}
	for _, obj := range output {
		kindCounts[obj.GetKind()]++
	}

	assert.Equal(t, 1, kindCounts["KongUpstream"])
	assert.Equal(t, 1, kindCounts["KongService"])
	assert.Equal(t, 2, kindCounts["KongRoute"])
	assert.Equal(t, 2, kindCounts["KongTarget"])
}

func TestHTTPRouteConverter_GetHybridGatewayParents(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name        string
		setup       func(t *testing.T) *httpRouteConverter
		wantLen     int
		wantErr     bool
		errContains string
		assertFn    func(t *testing.T, parents []hybridGatewayParent)
	}{
		{
			name: "returns supported parent with hostnames",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithHostnames("api.example.com")
				gateway := newGatewayWithListenerHostnames("api.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantLen: 1,
			assertFn: func(t *testing.T, parents []hybridGatewayParent) {
				assert.Equal(t, "test-gateway", string(parents[0].parentRef.Name))
				assert.NotNil(t, parents[0].cpRef)
				assert.Equal(t, []string{"api.example.com"}, parents[0].hostnames)
			},
		},
		{
			name: "skips parent with no matching hostnames",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithHostnames("api.example.com")
				gateway := newGatewayWithListenerHostnames("other.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
		},
		{
			name: "skips parent with unsupported group",
			setup: func(t *testing.T) *httpRouteConverter {
				invalidGroup := gwtypes.Group("invalid.group")
				gatewayKind := gwtypes.Kind("Gateway")
				route := newHTTPRouteWithHostnames("api.example.com")
				route.Spec.ParentRefs = []gwtypes.ParentReference{{
					Name:  "test-gateway",
					Group: &invalidGroup,
					Kind:  &gatewayKind,
				}}
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
		},
		{
			name: "skips parent with unsupported kind",
			setup: func(t *testing.T) *httpRouteConverter {
				gatewayGroup := gwtypes.Group(gwtypes.GroupName)
				invalidKind := gwtypes.Kind("ConfigMap")
				route := newHTTPRouteWithHostnames("api.example.com")
				route.Spec.ParentRefs = []gwtypes.ParentReference{{
					Name:  "test-gateway",
					Group: &gatewayGroup,
					Kind:  &invalidKind,
				}}
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
		},
		{
			name: "skips parent without control plane reference",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithHostnames("api.example.com")
				gateway := newGatewayWithListenerHostnames("api.example.com")
				gateway.UID = types.UID("gateway-uid")
				gatewayClass := &gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{Name: "test-gateway-class"},
					Spec:       gwtypes.GatewayClassSpec{ControllerName: "konghq.com/gateway-operator"},
				}
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(gateway, gatewayClass).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
		},
		{
			name: "returns error on gateway lookup failure",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithHostnames("api.example.com")
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*gwtypes.Gateway); ok {
								return fmt.Errorf("simulated gateway error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:     true,
			errContains: "failed to get ControlPlaneRef",
		},
		{
			name: "returns error when hostname lookup fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithHostnames("api.example.com")
				gateway := newGatewayWithListenerHostnames("api.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				getCount := 0
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(objects...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*gwtypes.Gateway); ok {
								getCount++
								if getCount > 1 {
									return fmt.Errorf("listener lookup error")
								}
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:     true,
			errContains: "listener lookup error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter := tt.setup(t)
			parents, err := getHybridGatewayParents(ctx, logr.Discard(), converter.Client, converter.route)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Len(t, parents, tt.wantLen)
			if tt.assertFn != nil {
				tt.assertFn(t, parents)
			}
		})
	}
}

func TestHTTPRouteConverter_Translate(t *testing.T) {
	type outputCount struct {
		upstreams int
		services  int
		routes    int
		targets   int
		bindings  int
		plugins   int
	}

	countOutputs := func(store []client.Object) outputCount {
		var counts outputCount
		for _, obj := range store {
			switch obj.(type) {
			case *configurationv1alpha1.KongUpstream:
				counts.upstreams++
			case *configurationv1alpha1.KongService:
				counts.services++
			case *configurationv1alpha1.KongRoute:
				counts.routes++
			case *configurationv1alpha1.KongTarget:
				counts.targets++
			case *configurationv1alpha1.KongPluginBinding:
				counts.bindings++
			case *configurationv1.KongPlugin:
				counts.plugins++
			}
		}
		return counts
	}

	baseGateway := func() *gwtypes.Gateway {
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")
		return gateway
	}

	baseObjects := func(gateway *gwtypes.Gateway) []client.Object {
		return append(newKonnectGatewayStandardObjects(gateway),
			newNamespace(),
			newService("default"),
			newEndpointSlice("backend-service", "default", []string{"10.0.0.1"}),
		)
	}

	// translateAndFindTarget runs Translate on a fresh converter built from objects and
	// returns the first KongTarget in the output store.
	translateAndFindTarget := func(t *testing.T, objects []client.Object) *configurationv1alpha1.KongTarget {
		t.Helper()
		route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
		conv := newHTTPRouteConverter(route, cl, false, "").(*httpRouteConverter)
		_, err := conv.Translate(t.Context(), logr.Discard())
		require.NoError(t, err)
		for _, obj := range conv.outputStore {
			if kt, ok := obj.(*configurationv1alpha1.KongTarget); ok {
				return kt
			}
		}
		t.Fatal("no KongTarget produced")
		return nil
	}

	tests := []struct {
		name         string
		setup        func(t *testing.T) *httpRouteConverter
		wantCount    int
		wantErr      bool
		wantErrSub   string
		wantOutputs  outputCount
		wantStoreLen int
		assertFn     func(t *testing.T, store []client.Object)
	}{
		{
			name: "translates route with plugins and targets",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef(""),
				}, []gwtypes.HTTPRouteFilter{
					newRequestHeaderFilter("x-test", "true"),
					newExtensionRefFilter("ext-plugin"),
				})
				gateway := baseGateway()
				objects := append(baseObjects(gateway), &configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{Name: "ext-plugin", Namespace: "default"},
					PluginName: "rate-limiting",
				})
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount: 8,
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
				targets:   1,
				bindings:  2,
				plugins:   2,
			},
			wantStoreLen: 8,
		},
		{
			name: "translates nonexistent backend into request termination plugin",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef(""),
				}, nil)
				gateway := baseGateway()
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace())
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount: 5,
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
				targets:   0,
				bindings:  1,
				plugins:   1,
			},
			wantStoreLen: 5,
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()

				var (
					serviceObj *configurationv1alpha1.KongService
					pluginObj  *configurationv1.KongPlugin
					bindingObj *configurationv1alpha1.KongPluginBinding
				)

				for _, obj := range store {
					switch typed := obj.(type) {
					case *configurationv1alpha1.KongService:
						serviceObj = typed
					case *configurationv1.KongPlugin:
						pluginObj = typed
					case *configurationv1alpha1.KongPluginBinding:
						bindingObj = typed
					}
				}

				require.NotNil(t, serviceObj)
				serviceName := serviceObj.Name
				normalRoute := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef("backend"),
				}, nil)
				normalServiceName := namegen.NewKongServiceNameForHTTPRouteRule(normalRoute, serviceObj.Spec.ControlPlaneRef, normalRoute.Spec.Rules[0])
				require.NotEmpty(t, serviceName)
				assert.NotEqual(t, normalServiceName, serviceName)
				require.NotNil(t, pluginObj)
				require.NotNil(t, bindingObj)
				assert.Equal(t, "request-termination", pluginObj.PluginName)

				var config map[string]any
				require.NoError(t, json.Unmarshal(pluginObj.Config.Raw, &config))
				statusCode, ok := config["status_code"].(float64)
				require.True(t, ok)
				assert.InDelta(t, 500, statusCode, 0)
				assert.Equal(t, "no existing backendRef provided", config["message"])

				require.NotNil(t, bindingObj.Spec.Targets)
				require.NotNil(t, bindingObj.Spec.Targets.ServiceReference)
				assert.Equal(t, serviceName, bindingObj.Spec.Targets.ServiceReference.Name)
				assert.Equal(t, pluginObj.Name, bindingObj.Spec.PluginReference.Name)
			},
		},
		{
			name: "translates cross-namespace backend without reference grant into request termination plugin",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef("backend"),
				}, nil)
				gateway := baseGateway()
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace(), newService("backend"))
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount: 5,
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
				targets:   0,
				bindings:  1,
				plugins:   1,
			},
			wantStoreLen: 5,
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()

				var (
					serviceName string
					pluginObj   *configurationv1.KongPlugin
					bindingObj  *configurationv1alpha1.KongPluginBinding
				)

				for _, obj := range store {
					switch typed := obj.(type) {
					case *configurationv1alpha1.KongService:
						serviceName = typed.Name
					case *configurationv1.KongPlugin:
						pluginObj = typed
					case *configurationv1alpha1.KongPluginBinding:
						bindingObj = typed
					}
				}

				require.NotEmpty(t, serviceName)
				require.NotNil(t, pluginObj)
				require.NotNil(t, bindingObj)
				assert.Equal(t, "request-termination", pluginObj.PluginName)

				var config map[string]any
				require.NoError(t, json.Unmarshal(pluginObj.Config.Raw, &config))
				statusCode, ok := config["status_code"].(float64)
				require.True(t, ok)
				assert.InDelta(t, 500, statusCode, 0)
				assert.Equal(t, "no existing backendRef provided", config["message"])

				require.NotNil(t, bindingObj.Spec.Targets)
				require.NotNil(t, bindingObj.Spec.Targets.ServiceReference)
				assert.Equal(t, serviceName, bindingObj.Spec.Targets.ServiceReference.Name)
				assert.Equal(t, pluginObj.Name, bindingObj.Spec.PluginReference.Name)
			},
		},
		{
			name: "translates partially invalid cross-namespace sibling backends",
			setup: func(t *testing.T) *httpRouteConverter {
				v1Name := gwtypes.ObjectName("app-backend-v1")
				v2Name := gwtypes.ObjectName("app-backend-v2")
				serviceKind := gwtypes.Kind("Service")
				serviceGroup := gwtypes.Group("")

				route := newHTTPRouteWithRules(
					[]string{"api.example.com"},
					[]gwtypes.HTTPRouteRule{
						{
							Matches: []gwtypes.HTTPRouteMatch{{
								Path: &gatewayv1.HTTPPathMatch{
									Type:  new(gatewayv1.PathMatchPathPrefix),
									Value: new("/v2"),
								},
							}},
							BackendRefs: []gwtypes.HTTPBackendRef{{
								BackendRef: gwtypes.BackendRef{
									BackendObjectReference: gwtypes.BackendObjectReference{
										Name:      v2Name,
										Namespace: new(gwtypes.Namespace("app-backend")),
										Kind:      &serviceKind,
										Group:     &serviceGroup,
										Port:      new(gwtypes.PortNumber(80)),
									},
								},
							}},
						},
						{
							Matches: []gwtypes.HTTPRouteMatch{{
								Path: &gatewayv1.HTTPPathMatch{
									Type:  new(gatewayv1.PathMatchPathPrefix),
									Value: new("/"),
								},
							}},
							BackendRefs: []gwtypes.HTTPBackendRef{{
								BackendRef: gwtypes.BackendRef{
									BackendObjectReference: gwtypes.BackendObjectReference{
										Name:      v1Name,
										Namespace: new(gwtypes.Namespace("app-backend")),
										Kind:      &serviceKind,
										Group:     &serviceGroup,
										Port:      new(gwtypes.PortNumber(80)),
									},
								},
							}},
						},
					},
				)

				gateway := baseGateway()
				objects := append(
					newKonnectGatewayStandardObjects(gateway),
					newNamespace(),
					&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{Name: "app-backend-v1", Namespace: "app-backend"},
						Spec: corev1.ServiceSpec{
							ClusterIP: "10.0.0.2",
							Ports: []corev1.ServicePort{{
								Name:       "http",
								Port:       80,
								Protocol:   corev1.ProtocolTCP,
								TargetPort: intstr.FromInt(8080),
							}},
						},
					},
					&corev1.Service{
						ObjectMeta: metav1.ObjectMeta{Name: "app-backend-v2", Namespace: "app-backend"},
						Spec: corev1.ServiceSpec{
							ClusterIP: "10.0.0.3",
							Ports: []corev1.ServicePort{{
								Name:       "http",
								Port:       80,
								Protocol:   corev1.ProtocolTCP,
								TargetPort: intstr.FromInt(8080),
							}},
						},
					},
					newEndpointSlice("app-backend-v1", "app-backend", []string{"10.0.1.1"}),
					&gwtypes.ReferenceGrant{
						ObjectMeta: metav1.ObjectMeta{Name: "allow-app-backend-v1", Namespace: "app-backend"},
						Spec: gwtypes.ReferenceGrantSpec{
							From: []gwtypes.ReferenceGrantFrom{{
								Group:     gwtypes.GroupName,
								Kind:      "HTTPRoute",
								Namespace: "default",
							}},
							To: []gwtypes.ReferenceGrantTo{{
								Group: gwtypes.Group(""),
								Kind:  gwtypes.Kind("Service"),
								Name:  &v1Name,
							}},
						},
					},
				)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount: 9,
			wantOutputs: outputCount{
				upstreams: 2,
				services:  2,
				routes:    2,
				targets:   1,
				bindings:  1,
				plugins:   1,
			},
			wantStoreLen: 9,
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()

				var (
					targets    []*configurationv1alpha1.KongTarget
					pluginObj  *configurationv1.KongPlugin
					bindingObj *configurationv1alpha1.KongPluginBinding
				)

				for _, obj := range store {
					switch typed := obj.(type) {
					case *configurationv1alpha1.KongTarget:
						targets = append(targets, typed)
					case *configurationv1.KongPlugin:
						pluginObj = typed
					case *configurationv1alpha1.KongPluginBinding:
						bindingObj = typed
					}
				}

				require.Len(t, targets, 1)
				assert.Equal(t, "10.0.1.1:8080", targets[0].Spec.Target)

				require.NotNil(t, pluginObj)
				require.NotNil(t, bindingObj)
				assert.Equal(t, "request-termination", pluginObj.PluginName)

				var config map[string]any
				require.NoError(t, json.Unmarshal(pluginObj.Config.Raw, &config))
				assert.Equal(t, "no existing backendRef provided", config["message"])
			},
		},
		{
			name: "translates unsupported backend kind into request termination plugin",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					func() gwtypes.HTTPBackendRef {
						ref := newBackendRef("")
						unsupportedKind := gwtypes.Kind("ConfigMap")
						ref.Kind = &unsupportedKind
						return ref
					}(),
				}, nil)
				gateway := baseGateway()
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace())
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount: 5,
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
				targets:   0,
				bindings:  1,
				plugins:   1,
			},
			wantStoreLen: 5,
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()

				var (
					serviceName string
					pluginObj   *configurationv1.KongPlugin
					bindingObj  *configurationv1alpha1.KongPluginBinding
				)

				for _, obj := range store {
					switch typed := obj.(type) {
					case *configurationv1alpha1.KongService:
						serviceName = typed.Name
					case *configurationv1.KongPlugin:
						pluginObj = typed
					case *configurationv1alpha1.KongPluginBinding:
						bindingObj = typed
					}
				}

				require.NotEmpty(t, serviceName)
				require.NotNil(t, pluginObj)
				require.NotNil(t, bindingObj)
				assert.Equal(t, "request-termination", pluginObj.PluginName)

				var config map[string]any
				require.NoError(t, json.Unmarshal(pluginObj.Config.Raw, &config))
				statusCode, ok := config["status_code"].(float64)
				require.True(t, ok)
				assert.InDelta(t, 500, statusCode, 0)
				assert.Equal(t, "no existing backendRef provided", config["message"])

				require.NotNil(t, bindingObj.Spec.Targets)
				require.NotNil(t, bindingObj.Spec.Targets.ServiceReference)
				assert.Equal(t, serviceName, bindingObj.Spec.Targets.ServiceReference.Name)
				assert.Equal(t, pluginObj.Name, bindingObj.Spec.PluginReference.Name)
			},
		},
		{
			name: "translates multi rule redirect only route end to end",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithRules(
					[]string{"api.example.com"},
					[]gwtypes.HTTPRouteRule{
						{
							Matches: []gwtypes.HTTPRouteMatch{{
								Path: &gatewayv1.HTTPPathMatch{
									Type:  new(gatewayv1.PathMatchPathPrefix),
									Value: new("/hostname-redirect"),
								},
							}},
							Filters: []gwtypes.HTTPRouteFilter{
								newRequestRedirectFilter("example.org", nil),
							},
						},
						{
							Matches: []gwtypes.HTTPRouteMatch{{
								Path: &gatewayv1.HTTPPathMatch{
									Type:  new(gatewayv1.PathMatchPathPrefix),
									Value: new("/host-and-status"),
								},
							}},
							Filters: []gwtypes.HTTPRouteFilter{
								newRequestRedirectFilter("example.org", new(301)),
							},
						},
					},
				)
				gateway := baseGateway()
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace())
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount: 10,
			wantOutputs: outputCount{
				upstreams: 2,
				services:  2,
				routes:    2,
				targets:   0,
				bindings:  2,
				plugins:   2,
			},
			wantStoreLen: 10,
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()

				upstreamNames := map[string]struct{}{}
				serviceNames := map[string]struct{}{}
				routeNames := map[string]struct{}{}
				pluginNames := map[string]struct{}{}
				bindingNames := map[string]struct{}{}

				pluginConfigs := map[string]map[string]any{}

				for _, obj := range store {
					switch typed := obj.(type) {
					case *configurationv1alpha1.KongUpstream:
						upstreamNames[typed.Name] = struct{}{}
					case *configurationv1alpha1.KongService:
						serviceNames[typed.Name] = struct{}{}
					case *configurationv1alpha1.KongRoute:
						routeNames[typed.Name] = struct{}{}
					case *configurationv1.KongPlugin:
						pluginNames[typed.Name] = struct{}{}
						var config map[string]any
						require.NoError(t, json.Unmarshal(typed.Config.Raw, &config))
						pluginConfigs[typed.Name] = config
					case *configurationv1alpha1.KongPluginBinding:
						bindingNames[typed.Name] = struct{}{}
						require.NotNil(t, typed.Spec.Targets)
						require.NotNil(t, typed.Spec.Targets.RouteReference)
						assert.Contains(t, routeNames, typed.Spec.Targets.RouteReference.Name)
						require.NotNil(t, typed.Spec.PluginReference)
						assert.Contains(t, pluginNames, typed.Spec.PluginReference.Name)
					}
				}

				assert.Len(t, upstreamNames, 2)
				assert.Len(t, serviceNames, 2)
				assert.Len(t, routeNames, 2)
				assert.Len(t, pluginNames, 2)
				assert.Len(t, bindingNames, 2)

				var sawDefaultStatus bool
				var sawCustomStatus bool
				for _, config := range pluginConfigs {
					assert.Equal(t, "http://example.org/", config["location"])
					assert.Equal(t, true, config["keep_incoming_path"])
					statusCode, ok := config["status_code"].(float64)
					require.True(t, ok)
					switch int(statusCode) {
					case 301:
						sawCustomStatus = true
					case 302:
						sawDefaultStatus = true
					}
				}
				assert.True(t, sawDefaultStatus)
				assert.True(t, sawCustomStatus)
			},
		},
		{
			name: "translates upstream header matching route end to end",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithRules(
					[]string{"api.example.com"},
					[]gwtypes.HTTPRouteRule{
						{
							Matches: []gwtypes.HTTPRouteMatch{{
								Headers: []gatewayv1.HTTPHeaderMatch{{
									Name:  "version",
									Value: "one",
								}},
							}},
							BackendRefs: []gwtypes.HTTPBackendRef{newBackendRef("")},
						},
						{
							Matches: []gwtypes.HTTPRouteMatch{{
								Headers: []gatewayv1.HTTPHeaderMatch{{
									Name:  "version",
									Value: "two",
								}},
							}},
							BackendRefs: []gwtypes.HTTPBackendRef{newBackendRef("")},
						},
						{
							Matches: []gwtypes.HTTPRouteMatch{{
								Headers: []gatewayv1.HTTPHeaderMatch{
									{
										Name:  "version",
										Value: "two",
									},
									{
										Name:  "color",
										Value: "orange",
									},
								},
							}},
							BackendRefs: []gwtypes.HTTPBackendRef{newBackendRef("")},
						},
						{
							Matches: []gwtypes.HTTPRouteMatch{
								{
									Headers: []gatewayv1.HTTPHeaderMatch{{
										Name:  "color",
										Value: "blue",
									}},
								},
								{
									Headers: []gatewayv1.HTTPHeaderMatch{{
										Name:  "color",
										Value: "green",
									}},
								},
							},
							BackendRefs: []gwtypes.HTTPBackendRef{newBackendRef("")},
						},
						{
							Matches: []gwtypes.HTTPRouteMatch{
								{
									Headers: []gatewayv1.HTTPHeaderMatch{{
										Name:  "color",
										Value: "red",
									}},
								},
								{
									Headers: []gatewayv1.HTTPHeaderMatch{{
										Name:  "color",
										Value: "yellow",
									}},
								},
							},
							BackendRefs: []gwtypes.HTTPBackendRef{newBackendRef("")},
						},
					},
				)
				gateway := baseGateway()
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(baseObjects(gateway)...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount: 10,
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    7,
				targets:   1,
			},
			wantStoreLen: 10,
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()

				routeNames := map[string]struct{}{}
				serviceNames := map[string]struct{}{}
				headersByRoute := map[string]int{}

				for _, obj := range store {
					route, ok := obj.(*configurationv1alpha1.KongRoute)
					if !ok {
						continue
					}

					routeNames[route.Name] = struct{}{}
					assert.Empty(t, route.Spec.Paths)
					assert.Empty(t, route.Spec.Methods)
					require.NotNil(t, route.Spec.ServiceRef)
					require.NotNil(t, route.Spec.ServiceRef.NamespacedRef)

					serviceName := route.Spec.ServiceRef.NamespacedRef.Name
					serviceNames[serviceName] = struct{}{}

					headersKey := canonicalHeaderMatchSet(route.Spec.Headers)
					headersByRoute[headersKey]++
				}

				expectedHeaders := map[string]int{
					"color=blue":               1,
					"color=green":              1,
					"color=orange&version=two": 1,
					"color=red":                1,
					"color=yellow":             1,
					"version=one":              1,
					"version=two":              1,
				}

				assert.Len(t, routeNames, 7)
				assert.Len(t, serviceNames, 1)
				assert.Equal(t, expectedHeaders, headersByRoute)
			},
		},
		{
			name: "returns error when filter translation fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef(""),
				}, []gwtypes.HTTPRouteFilter{{
					Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				}})
				gateway := baseGateway()
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(baseObjects(gateway)...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:    true,
			wantErrSub: "failed to translate KongPlugin for filter",
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
				targets:   1,
			},
			wantStoreLen: 4,
		},
		{
			name: "no supported parents produces no output",
			setup: func(t *testing.T) *httpRouteConverter {
				invalidGroup := gwtypes.Group("invalid.group")
				gatewayKind := gwtypes.Kind("Gateway")
				route := newHTTPRouteWithHostnames("api.example.com")
				route.Spec.ParentRefs = []gwtypes.ParentReference{{
					Name:  "missing-gateway",
					Group: &invalidGroup,
					Kind:  &gatewayKind,
				}}
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
		},
		{
			name: "returns error when parent lookup fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteWithHostnames("api.example.com")
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*gwtypes.Gateway); ok {
								return fmt.Errorf("simulated gateway error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:    true,
			wantErrSub: "failed to get ControlPlaneRef",
		},
		{
			name: "returns error when upstream translation fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := baseGateway()
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(baseObjects(gateway)...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*configurationv1alpha1.KongUpstream); ok {
								return fmt.Errorf("upstream get error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:    true,
			wantErrSub: "failed to translate KongUpstream resource",
		},
		{
			name: "returns error when service translation fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := baseGateway()
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(baseObjects(gateway)...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*configurationv1alpha1.KongService); ok {
								return fmt.Errorf("service get error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:      true,
			wantErrSub:   "failed to translate KongService for rule",
			wantStoreLen: 0,
		},
		{
			name: "returns error when route translation fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := baseGateway()
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(baseObjects(gateway)...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*configurationv1alpha1.KongRoute); ok {
								return fmt.Errorf("route get error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:      true,
			wantErrSub:   "failed to translate KongRoutes for rule",
			wantStoreLen: 0,
		},
		{
			name: "returns error when plugin binding fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, []gwtypes.HTTPRouteFilter{
					newRequestHeaderFilter("x-test", "true"),
				})
				gateway := baseGateway()
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(baseObjects(gateway)...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*configurationv1alpha1.KongPluginBinding); ok {
								return fmt.Errorf("binding get error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:    true,
			wantErrSub: "failed to build KongPluginBinding for plugin",
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
				targets:   1,
				plugins:   1,
			},
			wantStoreLen: 5,
		},
		{
			name: "returns error when targets translation fails",
			setup: func(t *testing.T) *httpRouteConverter {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef("backend"),
				}, nil)
				gateway := baseGateway()
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace(), newService("backend"))
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(objects...).
					WithInterceptorFuncs(interceptor.Funcs{
						List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							if _, ok := list.(*gwtypes.ReferenceGrantList); ok {
								return fmt.Errorf("reference grant list error")
							}
							return cl.List(ctx, list, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantErr:      true,
			wantErrSub:   "failed to translate KongTarget resources for upstream",
			wantStoreLen: 0,
		},
		{
			// When a KongTarget already exists in-cluster for the same (upstream, address), the
			// translator must reuse its name so the reconciler issues an UPDATE (not a
			// CREATE-then-duplicate) — the core invariant added by existingTargetNamesByAddress.
			name: "existing KongTarget name is reused on re-translate",
			setup: func(t *testing.T) *httpRouteConverter {
				gateway := baseGateway()
				objects := baseObjects(gateway)

				// First pass: discover the labels/annotations/upstream the translator assigns.
				firstTarget := translateAndFindTarget(t, objects)

				// Pre-seed the client with the same target under a legacy name — simulating an
				// in-cluster resource left over from a previous install with a different naming scheme.
				legacy := firstTarget.DeepCopy()
				legacy.Name = "legacy-target-name"

				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(append(objects, legacy)...).Build()
				return newHTTPRouteConverter(route, cl, false, "").(*httpRouteConverter)
			},
			wantCount:    4,
			wantStoreLen: 4,
			wantOutputs:  outputCount{upstreams: 1, services: 1, routes: 1, targets: 1},
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()
				var targets []*configurationv1alpha1.KongTarget
				for _, obj := range store {
					if kt, ok := obj.(*configurationv1alpha1.KongTarget); ok {
						targets = append(targets, kt)
					}
				}
				require.Len(t, targets, 1)
				assert.Equal(t, "legacy-target-name", targets[0].Name,
					"translator must reuse the pre-existing target name instead of minting a new one")
			},
		},
		{
			// When two KongTargets exist for the same address (broken/duplicate state), the
			// translator must prefer the Programmed one so the desired state equals the live
			// one and the non-programmed duplicate becomes an orphan to be cleaned up.
			name: "Programmed duplicate KongTarget name is preferred over non-programmed one",
			setup: func(t *testing.T) *httpRouteConverter {
				gateway := baseGateway()
				objects := baseObjects(gateway)

				// First pass: discover the labels/annotations/upstream the translator assigns.
				firstTarget := translateAndFindTarget(t, objects)

				programmedDup := firstTarget.DeepCopy()
				programmedDup.Name = "zzz-programmed-name" // larger name, must win
				programmedDup.Status.Conditions = []metav1.Condition{{
					Type:               "Programmed",
					Status:             metav1.ConditionTrue,
					Reason:             "Test",
					LastTransitionTime: metav1.Now(),
				}}

				failedDup := firstTarget.DeepCopy()
				failedDup.Name = "aaa-failed-name" // smaller name, must lose
				failedDup.Status.Conditions = []metav1.Condition{{
					Type:               "Programmed",
					Status:             metav1.ConditionFalse,
					Reason:             "Test",
					LastTransitionTime: metav1.Now(),
				}}

				cl := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(append(objects, programmedDup, failedDup)...).
					WithStatusSubresource(programmedDup, failedDup).
					Build()
				require.NoError(t, cl.Status().Update(t.Context(), programmedDup))
				require.NoError(t, cl.Status().Update(t.Context(), failedDup))

				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				return newHTTPRouteConverter(route, cl, false, "").(*httpRouteConverter)
			},
			wantCount:    4,
			wantStoreLen: 4,
			wantOutputs:  outputCount{upstreams: 1, services: 1, routes: 1, targets: 1},
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()
				var targets []*configurationv1alpha1.KongTarget
				for _, obj := range store {
					if kt, ok := obj.(*configurationv1alpha1.KongTarget); ok {
						targets = append(targets, kt)
					}
				}
				require.Len(t, targets, 1)
				assert.Equal(t, "zzz-programmed-name", targets[0].Name,
					"Programmed target name must win even when a smaller name exists for the same address")
			},
		},
		{
			name: "two backendRefs selecting the same pod IP produce one merged KongTarget",
			setup: func(t *testing.T) *httpRouteConverter {
				serviceKind := gwtypes.Kind("Service")
				serviceGroup := gwtypes.Group("")
				w100 := int32(100)
				w0 := int32(0)
				route := newHTTPRouteForTranslation(
					[]string{"api.example.com"},
					[]gwtypes.HTTPBackendRef{
						{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:  "svc-active",
									Kind:  &serviceKind,
									Group: &serviceGroup,
									Port:  new(gwtypes.PortNumber(80)),
								},
								Weight: &w100,
							},
						},
						{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:  "svc-preview",
									Kind:  &serviceKind,
									Group: &serviceGroup,
									Port:  new(gwtypes.PortNumber(80)),
								},
								Weight: &w0,
							},
						},
					},
					nil,
				)
				gateway := baseGateway()
				// Both services resolve to the same pod IP — the classic blue-green overlap scenario.
				svcActive := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-active", Namespace: "default"},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.0.1.1",
						Ports: []corev1.ServicePort{
							{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
						},
					},
				}
				svcPreview := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-preview", Namespace: "default"},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.0.1.2",
						Ports: []corev1.ServicePort{
							{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080)},
						},
					},
				}
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace(), svcActive, svcPreview,
					newEndpointSlice("svc-active", "default", []string{"10.0.0.1"}),
					newEndpointSlice("svc-preview", "default", []string{"10.0.0.1"}), // same pod IP
				)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
			},
			wantCount:    4,
			wantStoreLen: 4,
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
				targets:   1, // merged — not 2
			},
			assertFn: func(t *testing.T, store []client.Object) {
				t.Helper()
				var targets []*configurationv1alpha1.KongTarget
				for _, obj := range store {
					if tgt, ok := obj.(*configurationv1alpha1.KongTarget); ok {
						targets = append(targets, tgt)
					}
				}
				require.Len(t, targets, 1)
				assert.Equal(t, "10.0.0.1:8080", targets[0].Spec.Target)
				assert.Positive(t, targets[0].Spec.Weight)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter := tt.setup(t)
			count, err := converter.Translate(t.Context(), logr.Discard())
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSub)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantCount, count)
			if tt.wantStoreLen > 0 {
				assert.Len(t, converter.outputStore, tt.wantStoreLen)
			}
			if (tt.wantOutputs != outputCount{}) {
				assert.Equal(t, tt.wantOutputs, countOutputs(converter.outputStore))
			}
			if tt.assertFn != nil {
				tt.assertFn(t, converter.outputStore)
			}
		})
	}
}

func TestHTTPRouteConverter_UpdateRootObjectStatus(t *testing.T) {
	baseGateway := func() *gwtypes.Gateway {
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")
		return gateway
	}

	baseObjects := func(gateway *gwtypes.Gateway, route *gwtypes.HTTPRoute) []client.Object {
		return append(newKonnectGatewayStandardObjects(gateway), newNamespace(), newService("default"), route)
	}

	tests := []struct {
		name        string
		setup       func() (*httpRouteConverter, *gwtypes.HTTPRoute)
		wantUpdated bool
		wantStop    bool
		wantErr     bool
		errContains string
		assertFn    func(t *testing.T, route *gwtypes.HTTPRoute)
	}{
		{
			name: "updates accepted and resolved refs conditions",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef(""),
				}, nil)
				gateway := baseGateway()
				objects := baseObjects(gateway, route)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithStatusSubresource(route).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantUpdated: true,
			assertFn: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				conditions := route.Status.Parents[0].Conditions
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionAccepted), metav1.ConditionTrue)
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionTrue)
			},
		},
		{
			name: "resolved refs false does not set stop",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef("backend"),
				}, nil)
				gateway := baseGateway()
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace(), newService("backend"), route)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithStatusSubresource(route).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantUpdated: true,
			assertFn: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				conditions := route.Status.Parents[0].Conditions
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionFalse)
			},
		},
		{
			name: "nonexistent backend sets BackendNotFound without stop",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef(""),
				}, nil)
				gateway := baseGateway()
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace(), route)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithStatusSubresource(route).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantUpdated: true,
			assertFn: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				conditions := route.Status.Parents[0].Conditions
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionAccepted), metav1.ConditionTrue)
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionFalse)

				for _, condition := range conditions {
					if condition.Type == string(gwtypes.RouteConditionResolvedRefs) {
						assert.Equal(t, string(gwtypes.RouteReasonBackendNotFound), condition.Reason)
						return
					}
				}
				t.Fatalf("missing %s condition", gwtypes.RouteConditionResolvedRefs)
			},
		},
		{
			name: "accepted false sets stop",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef(""),
				}, nil)
				gateway := newGatewayWithListenerHostnames("other.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := baseObjects(gateway, route)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithStatusSubresource(route).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantUpdated: true,
			wantStop:    true,
			assertFn: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				conditions := route.Status.Parents[0].Conditions
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionAccepted), metav1.ConditionFalse)
			},
		},
		{
			name: "resolved refs true with reference grant",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef("backend"),
				}, nil)
				gateway := baseGateway()
				referenceGrant := &gwtypes.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{Name: "allow-backend", Namespace: "backend"},
					Spec: gwtypes.ReferenceGrantSpec{
						From: []gwtypes.ReferenceGrantFrom{{
							Group:     gatewayv1.GroupName,
							Kind:      gatewayv1.Kind("HTTPRoute"),
							Namespace: gatewayv1.Namespace("default"),
						}},
						To: []gwtypes.ReferenceGrantTo{{
							Group: "",
							Kind:  gatewayv1.Kind("Service"),
							Name:  new(gatewayv1.ObjectName("backend-service")),
						}},
					},
				}
				objects := append(newKonnectGatewayStandardObjects(gateway), newNamespace(), newService("backend"), referenceGrant, route)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithStatusSubresource(route).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantUpdated: true,
			assertFn: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				conditions := route.Status.Parents[0].Conditions
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionTrue)
			},
		},
		{
			name: "status update failure returns error",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := baseGateway()
				objects := baseObjects(gateway, route)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantErr:     true,
			errContains: "failed to update HTTPRoute status",
		},
		{
			name: "resolved refs condition build fails",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*corev1.Service); ok {
								return fmt.Errorf("service get error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantErr:     true,
			errContains: "failed to build resolvedRefs condition",
		},
		{
			name: "accepted condition build fails",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := baseGateway()
				objects := baseObjects(gateway, route)
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithStatusSubresource(route).
					WithObjects(objects...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*corev1.Namespace); ok {
								return fmt.Errorf("namespace get error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantErr:     true,
			errContains: "failed to build accepted condition",
		},
		{
			name: "programmed condition build fails",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := baseGateway()
				objects := baseObjects(gateway, route)
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithStatusSubresource(route).
					WithObjects(objects...).
					WithInterceptorFuncs(interceptor.Funcs{
						List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							if unstructuredList, ok := list.(*unstructured.UnstructuredList); ok {
								if unstructuredList.GroupVersionKind().Kind == "KongRoute" {
									return fmt.Errorf("programmed list error")
								}
							}
							return cl.List(ctx, list, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantErr:     true,
			errContains: "failed to build programmed condition",
		},
		{
			name: "removes status for unsupported parent",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				invalidGroup := gwtypes.Group("invalid.group")
				gatewayKind := gwtypes.Kind("Gateway")
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				pRef := gwtypes.ParentReference{Name: "test-gateway", Group: &invalidGroup, Kind: &gatewayKind}
				route.Spec.ParentRefs = []gwtypes.ParentReference{pRef}
				route.Status.Parents = []gwtypes.RouteParentStatus{
					{
						ParentRef:      pRef,
						ControllerName: gwtypes.GatewayController(vars.ControllerName()),
						Conditions:     []metav1.Condition{{Type: string(gwtypes.RouteConditionAccepted), Status: metav1.ConditionTrue}},
					},
				}
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithStatusSubresource(route).WithObjects(route).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantUpdated: true,
		},
		{
			name: "skips gateway without control plane reference",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := newGatewayWithListenerHostnames("api.example.com")
				gateway.UID = types.UID("gateway-uid")
				gatewayClass := &gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{Name: "test-gateway-class"},
					Spec:       gwtypes.GatewayClassSpec{ControllerName: "konghq.com/gateway-operator"},
				}
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(gateway, gatewayClass).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
		},
		{
			name: "cleans orphaned parent status",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				gateway := baseGateway()
				orphanParent := gwtypes.ParentReference{Name: "orphan", Group: new(gwtypes.Group(gwtypes.GroupName)), Kind: new(gwtypes.Kind("Gateway"))}
				route.Status.Parents = []gwtypes.RouteParentStatus{
					{
						ParentRef:      orphanParent,
						ControllerName: gwtypes.GatewayController(vars.ControllerName()),
						Conditions:     []metav1.Condition{{Type: string(gwtypes.RouteConditionAccepted), Status: metav1.ConditionTrue}},
					},
				}
				objects := baseObjects(gateway, route)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithStatusSubresource(route).WithObjects(objects...).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantUpdated: true,
		},
		{
			name: "returns error when gateway lookup fails",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{newBackendRef("")}, nil)
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*gwtypes.Gateway); ok {
								return fmt.Errorf("gateway get error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), route
			},
			wantErr:     true,
			errContains: "failed to get supported gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter, route := tt.setup()
			updated, stop, err := converter.UpdateRootObjectStatus(t.Context(), logr.Discard())
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUpdated, updated)
			assert.Equal(t, tt.wantStop, stop)
			if tt.assertFn != nil {
				tt.assertFn(t, route)
			}
		})
	}
}

func TestHTTPRouteConverter_HandleOrphanedResource(t *testing.T) {
	route := newHTTPRouteForTranslation([]string{"api.example.com"}, nil, nil)

	tests := []struct {
		name        string
		setup       func() (*httpRouteConverter, *unstructured.Unstructured)
		wantErr     bool
		wantSkip    bool
		errContains string
		assertFn    func(t *testing.T, resource *unstructured.Unstructured)
	}{
		{
			name: "skips resource without route annotation",
			setup: func() (*httpRouteConverter, *unstructured.Unstructured) {
				resource := newUnstructuredResource("")
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), resource
			},
			wantSkip: true,
		},
		{
			name: "updates annotation when other routes remain",
			setup: func() (*httpRouteConverter, *unstructured.Unstructured) {
				routeKey := client.ObjectKeyFromObject(route).String()
				resource := newUnstructuredResource(routeKey + ",other-ns/other-route")
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), resource
			},
			wantSkip: true,
			assertFn: func(t *testing.T, resource *unstructured.Unstructured) {
				assert.Equal(t, "other-ns/other-route", resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
			},
		},
		{
			name: "allows deletion when only route remains",
			setup: func() (*httpRouteConverter, *unstructured.Unstructured) {
				routeKey := client.ObjectKeyFromObject(route).String()
				resource := newUnstructuredResource(routeKey)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), resource
			},
			assertFn: func(t *testing.T, resource *unstructured.Unstructured) {
				_, exists := resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
				assert.False(t, exists)
			},
		},
		{
			name: "returns error when patch fails",
			setup: func() (*httpRouteConverter, *unstructured.Unstructured) {
				routeKey := client.ObjectKeyFromObject(route).String()
				resource := newUnstructuredResource(routeKey + ",other-ns/other-route")
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(resource).
					WithInterceptorFuncs(interceptor.Funcs{
						Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
							return fmt.Errorf("simulated patch error")
						},
					}).
					Build()
				return newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter), resource
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

func TestHTTPRouteConverter_GetHostnamesByParentRef(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name        string
		setup       func() (*httpRouteConverter, gwtypes.ParentReference)
		wantErr     bool
		errContains string
		wantHosts   []string
		wantNil     bool
	}{
		{
			name: "returns nil when section name mismatches",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames("api.example.com")
				gateway := newGatewayWithListenerHostnames("api.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				sectionName := gwtypes.SectionName("listener-1")
				port := gwtypes.PortNumber(80)
				pRef := gwtypes.ParentReference{
					Name:        "test-gateway",
					SectionName: &sectionName,
					Port:        &port,
					Group:       new(gwtypes.Group(gwtypes.GroupName)),
					Kind:        new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantNil: true,
		},
		{
			name: "returns hostnames when section matches",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames("api.example.com", "web.example.com")
				gateway := newGatewayWithListenerHostnames("*.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				sectionName := gwtypes.SectionName("listener-0")
				port := gwtypes.PortNumber(80)
				pRef := gwtypes.ParentReference{
					Name:        "test-gateway",
					SectionName: &sectionName,
					Port:        &port,
					Group:       new(gwtypes.Group(gwtypes.GroupName)),
					Kind:        new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantHosts: []string{"api.example.com", "web.example.com"},
		},
		{
			name: "returns nil when no hostname intersection",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames("api.example.com")
				gateway := newGatewayWithListenerHostnames("other.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				port := gwtypes.PortNumber(80)
				pRef := gwtypes.ParentReference{
					Name:  "test-gateway",
					Port:  &port,
					Group: new(gwtypes.Group(gwtypes.GroupName)),
					Kind:  new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantNil: true,
		},
		{
			name: "returns nil when port mismatches",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames("api.example.com")
				gateway := newGatewayWithListenerHostnames("api.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				sectionName := gwtypes.SectionName("listener-0")
				port := gwtypes.PortNumber(81)
				pRef := gwtypes.ParentReference{
					Name:        "test-gateway",
					SectionName: &sectionName,
					Port:        &port,
					Group:       new(gwtypes.Group(gwtypes.GroupName)),
					Kind:        new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantNil: true,
		},
		{
			name: "listener accepts all hostnames",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames("api.example.com", "web.example.com")
				gateway := newGatewayWithListenerHostnames()
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				port := gwtypes.PortNumber(80)
				pRef := gwtypes.ParentReference{
					Name:  "test-gateway",
					Port:  &port,
					Group: new(gwtypes.Group(gwtypes.GroupName)),
					Kind:  new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantHosts: []string{"api.example.com", "web.example.com"},
		},
		{
			name: "route without hostnames uses listener hostnames",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames()
				gateway := newGatewayWithListenerHostnames("api.example.com", "web.example.com")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				port := gwtypes.PortNumber(80)
				pRef := gwtypes.ParentReference{
					Name:  "test-gateway",
					Port:  &port,
					Group: new(gwtypes.Group(gwtypes.GroupName)),
					Kind:  new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantHosts: []string{"api.example.com", "web.example.com"},
		},
		{
			name: "listener accepts all hostnames with empty route hostnames",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames()
				gateway := newGatewayWithListenerHostnames()
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				port := gwtypes.PortNumber(80)
				pRef := gwtypes.ParentReference{
					Name:  "test-gateway",
					Port:  &port,
					Group: new(gwtypes.Group(gwtypes.GroupName)),
					Kind:  new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantHosts: []string{},
		},
		{
			name: "returns listener hostname when route hostnames are empty and section name matches without port",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames()
				gateway := newGatewayWithListenerHostnames("second-example.org")
				gateway.UID = types.UID("gateway-uid")
				objects := newKonnectGatewayStandardObjects(gateway)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				sectionName := gwtypes.SectionName("listener-0")
				pRef := gwtypes.ParentReference{
					Name:        "test-gateway",
					SectionName: &sectionName,
					Group:       new(gwtypes.Group(gwtypes.GroupName)),
					Kind:        new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantHosts: []string{"second-example.org"},
		},
		{
			name: "returns error when listener lookup fails",
			setup: func() (*httpRouteConverter, gwtypes.ParentReference) {
				route := newHTTPRouteWithHostnames("api.example.com")
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*gwtypes.Gateway); ok {
								return fmt.Errorf("listener lookup error")
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)
				port := gwtypes.PortNumber(80)
				pRef := gwtypes.ParentReference{
					Name:  "test-gateway",
					Port:  &port,
					Group: new(gwtypes.Group(gwtypes.GroupName)),
					Kind:  new(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantErr:     true,
			errContains: "listener lookup error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter, pRef := tt.setup()
			hostnames, err := getHostnamesByParentRef(ctx, logr.Discard(), converter.Client, converter.route, pRef)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, hostnames)
				return
			}
			assert.ElementsMatch(t, tt.wantHosts, hostnames)
		})
	}
}

func TestHTTPRouteConverter_MetadataAccessors(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "returns root and output metadata"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := newHTTPRouteWithHostnames("api.example.com")
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
			converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

			converter.outputStore = []client.Object{
				&configurationv1alpha1.KongRoute{ObjectMeta: metav1.ObjectMeta{Name: "route"}},
			}

			root := converter.GetRootObject()
			assert.Equal(t, route.Name, root.Name)
			assert.Equal(t, 1, converter.GetOutputStoreLen(t.Context(), logr.Discard()))
			assert.NotEmpty(t, converter.GetExpectedGVKs())
		})
	}
}

func assertConditionStatus(t *testing.T, conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus) {
	t.Helper()
	for _, cond := range conditions {
		if cond.Type == conditionType {
			assert.Equal(t, status, cond.Status)
			return
		}
	}
	t.Fatalf("condition %s not found", conditionType)
}

func newHTTPRouteForTranslation(hostnames []string, backendRefs []gwtypes.HTTPBackendRef, filters []gwtypes.HTTPRouteFilter) *gwtypes.HTTPRoute {
	return newHTTPRouteWithRules(hostnames, []gwtypes.HTTPRouteRule{{
		Matches: []gwtypes.HTTPRouteMatch{{
			Path: &gatewayv1.HTTPPathMatch{
				Type:  new(gatewayv1.PathMatchPathPrefix),
				Value: new("/"),
			},
		}},
		BackendRefs: backendRefs,
		Filters:     filters,
	}})
}

func canonicalHeaderMatchSet(headers map[string][]string) string {
	if len(headers) == 0 {
		return ""
	}

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		values := append([]string(nil), headers[key]...)
		slices.Sort(values)
		parts = append(parts, key+"="+strings.Join(values, "|"))
	}

	return strings.Join(parts, "&")
}

func newHTTPRouteWithRules(hostnames []string, rules []gwtypes.HTTPRouteRule) *gwtypes.HTTPRoute {
	var gwHostnames []gatewayv1.Hostname
	for _, hostname := range hostnames {
		gwHostnames = append(gwHostnames, gatewayv1.Hostname(hostname))
	}

	return &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Hostnames: gwHostnames,
			Rules:     rules,
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{
					{
						Name:  "test-gateway",
						Kind:  new(gwtypes.Kind("Gateway")),
						Group: new(gwtypes.Group(gwtypes.GroupName)),
					},
				},
			},
		},
	}
}

func newRequestRedirectFilter(hostname string, statusCode *int) gwtypes.HTTPRouteFilter {
	filter := gwtypes.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterRequestRedirect,
		RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
			Hostname: new(gatewayv1.PreciseHostname(hostname)),
		},
	}
	if statusCode != nil {
		filter.RequestRedirect.StatusCode = statusCode
	}
	return filter
}

func newBackendRef(namespace string) gwtypes.HTTPBackendRef {
	serviceKind := gwtypes.Kind("Service")
	serviceGroup := gwtypes.Group("")
	ref := gwtypes.HTTPBackendRef{
		BackendRef: gwtypes.BackendRef{
			BackendObjectReference: gwtypes.BackendObjectReference{
				Name:  "backend-service",
				Kind:  &serviceKind,
				Group: &serviceGroup,
				Port:  new(gwtypes.PortNumber(80)),
			},
		},
	}

	if namespace != "" {
		ns := gwtypes.Namespace(namespace)
		ref.Namespace = &ns
	}

	return ref
}

func newRequestHeaderFilter(name, value string) gwtypes.HTTPRouteFilter {
	return gwtypes.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
		RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
			Set: []gatewayv1.HTTPHeader{
				{
					Name:  gatewayv1.HTTPHeaderName(name),
					Value: value,
				},
			},
		},
	}
}

func newExtensionRefFilter(name string) gwtypes.HTTPRouteFilter {
	return gwtypes.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterExtensionRef,
		ExtensionRef: &gatewayv1.LocalObjectReference{
			Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
			Kind:  "KongPlugin",
			Name:  gatewayv1.ObjectName(name),
		},
	}
}

func newService(namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-service",
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
}

func newEndpointSlice(serviceName, namespace string, addresses []string) *discoveryv1.EndpointSlice {
	port := int32(8080)
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-slice", serviceName),
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: serviceName,
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{
				Name:     new("http"),
				Port:     new(port),
				Protocol: new(corev1.ProtocolTCP),
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: addresses,
				Conditions: discoveryv1.EndpointConditions{
					Ready: new(true),
				},
			},
		},
	}
}

func newNamespace() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}
}

func newUnstructuredResource(routesAnnotation string) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongService"))
	resource.SetName("orphaned")
	resource.SetNamespace("default")
	if routesAnnotation != "" {
		resource.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: routesAnnotation,
		})
	}
	return resource
}

func TestHTTPRouteConverter_DesiredResourcesReady(t *testing.T) {
	ctx := t.Context()
	const (
		ns           = "default"
		routeName    = "route-1"
		svcName      = "svc-1"
		konnectSvcID = "konnect-svc-id-abc"
	)

	routeGVK := configurationv1alpha1.GroupVersion.WithKind("KongRoute")

	// desiredRoute builds a KongRoute for the converter's outputStore.
	desiredRoute := func(name string) *configurationv1alpha1.KongRoute {
		r := &configurationv1alpha1.KongRoute{}
		r.Name = name
		r.Namespace = ns
		r.SetGroupVersionKind(routeGVK)
		return r
	}

	// clusterRoute builds an unstructured KongRoute representing what is in the cluster.
	clusterRoute := func(name, serviceRefName, boundServiceID string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(routeGVK)
		u.SetName(name)
		u.SetNamespace(ns)
		if serviceRefName != "" {
			_ = unstructured.SetNestedField(u.Object, serviceRefName, "spec", "serviceRef", "namespacedRef", "name")
		}
		if boundServiceID != "" {
			_ = unstructured.SetNestedField(u.Object, boundServiceID, "status", "konnect", "serviceID")
		}
		return u
	}

	// kongService builds a typed KongService with configurable Programmed status and Konnect ID.
	kongService := func(name string, programmed bool, konnectID string) *configurationv1alpha1.KongService {
		svc := &configurationv1alpha1.KongService{}
		svc.Name = name
		svc.Namespace = ns
		if programmed {
			svc.Status.Conditions = []metav1.Condition{{
				Type:               "Programmed",
				Status:             metav1.ConditionTrue,
				Reason:             "Programmed",
				LastTransitionTime: metav1.Now(),
			}}
		}
		if konnectID != "" {
			svc.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: konnectID},
			}
		}
		return svc
	}

	baseRoute := &gwtypes.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: ns}}

	tests := []struct {
		name            string
		outputStore     []client.Object
		existingObjs    []client.Object
		interceptorFn   *interceptor.Funcs
		wantReady       bool
		wantErrContains string
	}{
		{
			name:            "GetOutputStore error is propagated",
			outputStore:     []client.Object{&badObject{Name: "bad"}},
			wantReady:       false,
			wantErrContains: "failed to get desired objects for readiness check",
		},
		{
			name:        "empty output store → ready",
			outputStore: nil,
			wantReady:   true,
		},
		{
			name: "no KongRoutes in output store → ready",
			outputStore: []client.Object{
				func() *configurationv1alpha1.KongService {
					s := &configurationv1alpha1.KongService{}
					s.Name = "svc-only"
					s.Namespace = ns
					s.SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongService"))
					return s
				}(),
			},
			wantReady: true,
		},
		{
			name:        "desired KongRoute not yet in cluster → defer (NotFound)",
			outputStore: []client.Object{desiredRoute(routeName)},
			wantReady:   false,
		},
		{
			name:        "Get KongRoute returns non-NotFound error → propagate",
			outputStore: []client.Object{desiredRoute(routeName)},
			interceptorFn: &interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*unstructured.Unstructured); ok && key.Name == routeName {
						return fmt.Errorf("simulated get error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantReady:       false,
			wantErrContains: "failed to get KongRoute",
		},
		{
			name:         "KongRoute found, no serviceRef (serviceless route) → ready",
			outputStore:  []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{clusterRoute(routeName, "", "")},
			wantReady:    true,
		},
		{
			name:         "KongRoute found with serviceRef, KongService not found → defer",
			outputStore:  []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{clusterRoute(routeName, svcName, "")},
			wantReady:    false,
		},
		{
			name:         "Get KongService returns non-NotFound error → propagate",
			outputStore:  []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{clusterRoute(routeName, svcName, "")},
			interceptorFn: &interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.KongService); ok {
						return fmt.Errorf("simulated service get error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantReady:       false,
			wantErrContains: "failed to get KongService",
		},
		{
			name:         "KongService found but not Programmed → defer",
			outputStore:  []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{clusterRoute(routeName, svcName, ""), kongService(svcName, false, "")},
			wantReady:    false,
		},
		{
			name:        "KongService Programmed but Konnect status nil (no Konnect ID) → defer",
			outputStore: []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{
				clusterRoute(routeName, svcName, konnectSvcID),
				kongService(svcName, true, ""), // Programmed but Konnect == nil
			},
			wantReady: false,
		},
		{
			name:        "KongService Programmed, Konnect ID set, but route bound to old service ID → defer",
			outputStore: []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{
				clusterRoute(routeName, svcName, "old-service-id"),
				kongService(svcName, true, konnectSvcID),
			},
			wantReady: false,
		},
		{
			name:        "KongRoute bound to correct service ID → ready",
			outputStore: []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{
				clusterRoute(routeName, svcName, konnectSvcID),
				kongService(svcName, true, konnectSvcID),
			},
			wantReady: true,
		},
		{
			name:         "multiple routes: first not ready → defer immediately",
			outputStore:  []client.Object{desiredRoute("route-a"), desiredRoute("route-b")},
			existingObjs: []client.Object{
				// route-a not in cluster → defers before even checking route-b
			},
			wantReady: false,
		},
		{
			name:        "multiple routes: all bound to correct service → ready",
			outputStore: []client.Object{desiredRoute("route-a"), desiredRoute("route-b")},
			existingObjs: []client.Object{
				clusterRoute("route-a", svcName, konnectSvcID),
				clusterRoute("route-b", svcName, konnectSvcID),
				kongService(svcName, true, konnectSvcID),
			},
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme.Get())
			if len(tt.existingObjs) > 0 {
				builder = builder.WithObjects(tt.existingObjs...)
			}
			if tt.interceptorFn != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptorFn)
			}
			cl := builder.Build()

			conv := newHTTPRouteConverter(baseRoute, cl, false, "").(*httpRouteConverter)
			conv.outputStore = tt.outputStore

			ready, err := conv.DesiredResourcesReady(ctx, logr.Discard())

			if tt.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantReady, ready)
		})
	}
}
