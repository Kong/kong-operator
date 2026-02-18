package converter

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

func TestHTTPRouteConverter_GetOutputStore(t *testing.T) {
	ctx := context.Background()
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

func TestHTTPRouteConverter_GetHybridGatewayParents(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		setup       func() *httpRouteConverter
		wantLen     int
		wantErr     bool
		errContains string
		assertFn    func(t *testing.T, parents []hybridGatewayParent)
	}{
		{
			name: "returns supported parent with hostnames",
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			converter := tt.setup()
			parents, err := converter.getHybridGatewayParents(ctx, logr.Discard())
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
			newEndpointSlice("backend-service", "default", 8080, []string{"10.0.0.1"}),
		)
	}

	tests := []struct {
		name         string
		setup        func() *httpRouteConverter
		wantCount    int
		wantErr      bool
		wantErrSub   string
		wantOutputs  outputCount
		wantStoreLen int
	}{
		{
			name: "translates route with plugins and targets",
			setup: func() *httpRouteConverter {
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
			name: "returns error when filter translation fails",
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			wantErr:    true,
			wantErrSub: "failed to translate KongService for rule",
			wantOutputs: outputCount{
				upstreams: 1,
			},
			wantStoreLen: 1,
		},
		{
			name: "returns error when route translation fails",
			setup: func() *httpRouteConverter {
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
			wantErr:    true,
			wantErrSub: "failed to translate KongRoute for rule",
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
			},
			wantStoreLen: 2,
		},
		{
			name: "returns error when plugin binding fails",
			setup: func() *httpRouteConverter {
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
			setup: func() *httpRouteConverter {
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
			wantErr:    true,
			wantErrSub: "failed to translate KongTarget resources for upstream",
			wantOutputs: outputCount{
				upstreams: 1,
				services:  1,
				routes:    1,
			},
			wantStoreLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter := tt.setup()
			count, err := converter.Translate(context.Background(), logr.Discard())
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
			name: "resolved refs false sets stop",
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
			wantStop:    true,
			assertFn: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				conditions := route.Status.Parents[0].Conditions
				assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionFalse)
			},
		},
		{
			name: "resolved refs true with reference grant",
			setup: func() (*httpRouteConverter, *gwtypes.HTTPRoute) {
				route := newHTTPRouteForTranslation([]string{"api.example.com"}, []gwtypes.HTTPBackendRef{
					newBackendRef("backend"),
				}, nil)
				gateway := baseGateway()
				referenceGrant := &gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{Name: "allow-backend", Namespace: "backend"},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{{
							Group:     gatewayv1.GroupName,
							Kind:      gatewayv1.Kind("HTTPRoute"),
							Namespace: gatewayv1.Namespace("default"),
						}},
						To: []gatewayv1beta1.ReferenceGrantTo{{
							Group: "",
							Kind:  gatewayv1.Kind("Service"),
							Name:  ptr.To(gatewayv1.ObjectName("backend-service")),
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
				orphanParent := gwtypes.ParentReference{Name: "orphan", Group: lo.ToPtr(gwtypes.Group(gwtypes.GroupName)), Kind: lo.ToPtr(gwtypes.Kind("Gateway"))}
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
			updated, stop, err := converter.UpdateRootObjectStatus(context.Background(), logr.Discard())
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
				assert.Equal(t, "other-ns/other-route", resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesAnnotation])
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
				_, exists := resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesAnnotation]
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
			skipDelete, err := converter.HandleOrphanedResource(context.Background(), logr.Discard(), resource)
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
	ctx := context.Background()

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
					Group:       lo.ToPtr(gwtypes.Group(gwtypes.GroupName)),
					Kind:        lo.ToPtr(gwtypes.Kind("Gateway")),
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
					Group:       lo.ToPtr(gwtypes.Group(gwtypes.GroupName)),
					Kind:        lo.ToPtr(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantHosts: []string{"api.example.com", "web.example.com"},
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
					Group:       lo.ToPtr(gwtypes.Group(gwtypes.GroupName)),
					Kind:        lo.ToPtr(gwtypes.Kind("Gateway")),
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
					Group: lo.ToPtr(gwtypes.Group(gwtypes.GroupName)),
					Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
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
					Group: lo.ToPtr(gwtypes.Group(gwtypes.GroupName)),
					Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
				}
				return converter, pRef
			},
			wantHosts: []string{},
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
					Group: lo.ToPtr(gwtypes.Group(gwtypes.GroupName)),
					Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
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
			hostnames, err := converter.getHostnamesByParentRef(ctx, logr.Discard(), pRef)
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
			assert.Equal(t, 1, converter.GetOutputStoreLen(context.Background(), logr.Discard()))
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
	var gwHostnames []gatewayv1.Hostname
	for _, hostname := range hostnames {
		gwHostnames = append(gwHostnames, gatewayv1.Hostname(hostname))
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
							Path: &gatewayv1.HTTPPathMatch{
								Type:  lo.ToPtr(gatewayv1.PathMatchPathPrefix),
								Value: lo.ToPtr("/"),
							},
						},
					},
					BackendRefs: backendRefs,
					Filters:     filters,
				},
			},
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{
					{
						Name:  "test-gateway",
						Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
						Group: lo.ToPtr(gwtypes.Group(gwtypes.GroupName)),
					},
				},
			},
		},
	}
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
				Port:  lo.ToPtr(gwtypes.PortNumber(80)),
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

func newEndpointSlice(serviceName, namespace string, port int32, addresses []string) *discoveryv1.EndpointSlice {
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
				Name:     ptr.To("http"),
				Port:     ptr.To(port),
				Protocol: ptr.To(corev1.ProtocolTCP),
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: addresses,
				Conditions: discoveryv1.EndpointConditions{
					Ready: ptr.To(true),
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
			consts.GatewayOperatorHybridRoutesAnnotation: routesAnnotation,
		})
	}
	return resource
}
