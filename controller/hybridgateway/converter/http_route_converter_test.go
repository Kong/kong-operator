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

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
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

	t.Run("all objects convert successfully", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newHTTPRouteConverter(&gwtypes.HTTPRoute{}, fakeClient, false, "").(*httpRouteConverter)
		converter.outputStore = []client.Object{validUpstream, validService}

		objects, err := converter.GetOutputStore(ctx, logger)
		require.NoError(t, err)
		require.Len(t, objects, 2)
		assert.Equal(t, "upstream-1", objects[0].GetName())
		assert.Equal(t, "service-1", objects[1].GetName())
	})

	t.Run("one object fails conversion", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newHTTPRouteConverter(&gwtypes.HTTPRoute{}, fakeClient, false, "").(*httpRouteConverter)
		badObj := &badObject{Name: "bad-1"}
		converter.outputStore = []client.Object{validUpstream, badObj, validService}

		objects, err := converter.GetOutputStore(ctx, logger)
		require.Error(t, err)
		require.Len(t, objects, 2)
		assert.Contains(t, err.Error(), "output store conversion failed with 1 errors")
		assert.Contains(t, err.Error(), "failed to convert *converter.badObject bad-1 to unstructured")
	})
}

func TestHTTPRouteConverter_GetHybridGatewayParents(t *testing.T) {
	ctx := context.Background()

	t.Run("returns supported parent with hostnames", func(t *testing.T) {
		route := newHTTPRouteWithHostnames("api.example.com")
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")

		objects := newKonnectGatewayStandardObjects(gateway)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		parents, err := converter.getHybridGatewayParents(ctx, logr.Discard())
		require.NoError(t, err)
		require.Len(t, parents, 1)
		assert.Equal(t, "test-gateway", string(parents[0].parentRef.Name))
		assert.NotNil(t, parents[0].cpRef)
		assert.Equal(t, []string{"api.example.com"}, parents[0].hostnames)
	})

	t.Run("skips parent with no matching hostnames", func(t *testing.T) {
		route := newHTTPRouteWithHostnames("api.example.com")
		gateway := newGatewayWithListenerHostnames("other.example.com")
		gateway.UID = types.UID("gateway-uid")

		objects := newKonnectGatewayStandardObjects(gateway)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		parents, err := converter.getHybridGatewayParents(ctx, logr.Discard())
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("skips parent with unsupported group", func(t *testing.T) {
		invalidGroup := gwtypes.Group("invalid.group")
		gatewayKind := gwtypes.Kind("Gateway")
		route := newHTTPRouteWithHostnames("api.example.com")
		route.Spec.ParentRefs = []gwtypes.ParentReference{
			{
				Name:  "test-gateway",
				Group: &invalidGroup,
				Kind:  &gatewayKind,
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		parents, err := converter.getHybridGatewayParents(ctx, logr.Discard())
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("skips parent with unsupported kind", func(t *testing.T) {
		gatewayGroup := gwtypes.Group(gwtypes.GroupName)
		invalidKind := gwtypes.Kind("ConfigMap")
		route := newHTTPRouteWithHostnames("api.example.com")
		route.Spec.ParentRefs = []gwtypes.ParentReference{
			{
				Name:  "test-gateway",
				Group: &gatewayGroup,
				Kind:  &invalidKind,
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		parents, err := converter.getHybridGatewayParents(ctx, logr.Discard())
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("skips parent without control plane reference", func(t *testing.T) {
		route := newHTTPRouteWithHostnames("api.example.com")
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")
		gatewayClass := &gwtypes.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-gateway-class",
			},
			Spec: gwtypes.GatewayClassSpec{
				ControllerName: "konghq.com/gateway-operator",
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(gateway, gatewayClass).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		parents, err := converter.getHybridGatewayParents(ctx, logr.Discard())
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("returns error on gateway lookup failure", func(t *testing.T) {
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
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		_, err := converter.getHybridGatewayParents(ctx, logr.Discard())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get ControlPlaneRef")
	})
}

func TestHTTPRouteConverter_Translate(t *testing.T) {
	t.Run("translates route with plugins and targets", func(t *testing.T) {
		route := newHTTPRouteForTranslation("default", []string{"api.example.com"}, []gwtypes.HTTPBackendRef{
			newBackendRef("backend-service", "", 80),
		}, []gwtypes.HTTPRouteFilter{
			newRequestHeaderFilter("x-test", "true"),
			newExtensionRefFilter("ext-plugin"),
		})
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")
		objects := append(newKonnectGatewayStandardObjects(gateway),
			newNamespace("default"),
			newService("backend-service", "default", 80, 8080),
			newEndpointSlice("backend-service", "default", 8080, []string{"10.0.0.1"}),
			&configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ext-plugin",
					Namespace: "default",
				},
				PluginName: "rate-limiting",
			},
		)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		count, err := converter.Translate(context.Background(), logr.Discard())
		require.NoError(t, err)
		assert.Equal(t, 7, count)

		var (
			upstreams int
			services  int
			routes    int
			targets   int
			bindings  int
			plugins   int
		)

		for _, obj := range converter.outputStore {
			switch obj.(type) {
			case *configurationv1alpha1.KongUpstream:
				upstreams++
			case *configurationv1alpha1.KongService:
				services++
			case *configurationv1alpha1.KongRoute:
				routes++
			case *configurationv1alpha1.KongTarget:
				targets++
			case *configurationv1alpha1.KongPluginBinding:
				bindings++
			case *configurationv1.KongPlugin:
				plugins++
			}
		}

		assert.Equal(t, 1, upstreams)
		assert.Equal(t, 1, services)
		assert.Equal(t, 1, routes)
		assert.Equal(t, 1, targets)
		assert.Equal(t, 2, bindings)
		assert.Equal(t, 1, plugins)
	})

	t.Run("returns error when filter translation fails", func(t *testing.T) {
		route := newHTTPRouteForTranslation("default", []string{"api.example.com"}, []gwtypes.HTTPBackendRef{
			newBackendRef("backend-service", "", 80),
		}, []gwtypes.HTTPRouteFilter{
			{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
			},
		})
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")
		objects := append(newKonnectGatewayStandardObjects(gateway),
			newNamespace("default"),
			newService("backend-service", "default", 80, 8080),
			newEndpointSlice("backend-service", "default", 8080, []string{"10.0.0.1"}),
		)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		count, err := converter.Translate(context.Background(), logr.Discard())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to translate KongPlugin for filter")
		assert.Equal(t, 0, count)
		assert.Len(t, converter.outputStore, 4)
	})

	t.Run("no supported parents produces no output", func(t *testing.T) {
		invalidGroup := gwtypes.Group("invalid.group")
		gatewayKind := gwtypes.Kind("Gateway")
		route := newHTTPRouteWithHostnames("api.example.com")
		route.Spec.ParentRefs = []gwtypes.ParentReference{
			{
				Name:  "missing-gateway",
				Group: &invalidGroup,
				Kind:  &gatewayKind,
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "")

		count, err := converter.Translate(context.Background(), logr.Discard())
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("returns error when parent lookup fails", func(t *testing.T) {
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
		converter := newHTTPRouteConverter(route, fakeClient, false, "")

		count, err := converter.Translate(context.Background(), logr.Discard())
		require.Error(t, err)
		assert.Equal(t, 0, count)
		assert.Contains(t, err.Error(), "failed to get ControlPlaneRef")
	})
}

func TestHTTPRouteConverter_UpdateRootObjectStatus(t *testing.T) {
	t.Run("updates accepted and resolved refs conditions", func(t *testing.T) {
		route := newHTTPRouteForTranslation("default", []string{"api.example.com"}, []gwtypes.HTTPBackendRef{
			newBackendRef("backend-service", "", 80),
		}, nil)
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")

		objects := append(newKonnectGatewayStandardObjects(gateway),
			newNamespace("default"),
			newService("backend-service", "default", 80, 8080),
			route,
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithStatusSubresource(route).
			WithObjects(objects...).
			Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		updated, stop, err := converter.UpdateRootObjectStatus(context.Background(), logr.Discard())
		require.NoError(t, err)
		assert.True(t, updated)
		assert.False(t, stop)

		require.Len(t, route.Status.Parents, 1)
		conditions := route.Status.Parents[0].Conditions
		assertConditionStatus(t, conditions, string(gwtypes.RouteConditionAccepted), metav1.ConditionTrue)
		assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionTrue)
	})

	t.Run("resolved refs false sets stop", func(t *testing.T) {
		route := newHTTPRouteForTranslation("default", []string{"api.example.com"}, []gwtypes.HTTPBackendRef{
			newBackendRef("backend-service", "backend", 80),
		}, nil)
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")

		objects := append(newKonnectGatewayStandardObjects(gateway),
			newNamespace("default"),
			newService("backend-service", "backend", 80, 8080),
			route,
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithStatusSubresource(route).
			WithObjects(objects...).
			Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		updated, stop, err := converter.UpdateRootObjectStatus(context.Background(), logr.Discard())
		require.NoError(t, err)
		assert.True(t, updated)
		assert.True(t, stop)

		require.Len(t, route.Status.Parents, 1)
		conditions := route.Status.Parents[0].Conditions
		assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionFalse)
	})

	t.Run("resolved refs true with reference grant", func(t *testing.T) {
		route := newHTTPRouteForTranslation("default", []string{"api.example.com"}, []gwtypes.HTTPBackendRef{
			newBackendRef("backend-service", "backend", 80),
		}, nil)
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")

		referenceGrant := &gatewayv1beta1.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "allow-backend",
				Namespace: "backend",
			},
			Spec: gatewayv1beta1.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{
					{
						Group:     gatewayv1.GroupName,
						Kind:      gatewayv1.Kind("HTTPRoute"),
						Namespace: gatewayv1.Namespace("default"),
					},
				},
				To: []gatewayv1beta1.ReferenceGrantTo{
					{
						Group: "",
						Kind:  gatewayv1.Kind("Service"),
						Name:  ptr.To(gatewayv1.ObjectName("backend-service")),
					},
				},
			},
		}

		objects := append(newKonnectGatewayStandardObjects(gateway),
			newNamespace("default"),
			newService("backend-service", "backend", 80, 8080),
			referenceGrant,
			route,
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithStatusSubresource(route).
			WithObjects(objects...).
			Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		updated, stop, err := converter.UpdateRootObjectStatus(context.Background(), logr.Discard())
		require.NoError(t, err)
		assert.True(t, updated)
		assert.False(t, stop)

		require.Len(t, route.Status.Parents, 1)
		conditions := route.Status.Parents[0].Conditions
		assertConditionStatus(t, conditions, string(gwtypes.RouteConditionResolvedRefs), metav1.ConditionTrue)
	})

	t.Run("status update failure returns error", func(t *testing.T) {
		route := newHTTPRouteForTranslation("default", []string{"api.example.com"}, []gwtypes.HTTPBackendRef{
			newBackendRef("backend-service", "", 80),
		}, nil)
		gateway := newGatewayWithListenerHostnames("api.example.com")
		gateway.UID = types.UID("gateway-uid")

		objects := append(newKonnectGatewayStandardObjects(gateway),
			newNamespace("default"),
			newService("backend-service", "default", 80, 8080),
			route,
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(objects...).
			Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		updated, stop, err := converter.UpdateRootObjectStatus(context.Background(), logr.Discard())
		require.Error(t, err)
		assert.False(t, updated)
		assert.False(t, stop)
		assert.Contains(t, err.Error(), "failed to update HTTPRoute status")
	})
}

func TestHTTPRouteConverter_HandleOrphanedResource(t *testing.T) {
	route := newHTTPRouteForTranslation("default", []string{"api.example.com"}, nil, nil)

	t.Run("skips resource without route annotation", func(t *testing.T) {
		resource := newUnstructuredResource("orphaned", "default", "")
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		skipDelete, err := converter.HandleOrphanedResource(context.Background(), logr.Discard(), resource)
		require.NoError(t, err)
		assert.True(t, skipDelete)
	})

	t.Run("updates annotation when other routes remain", func(t *testing.T) {
		routeKey := client.ObjectKeyFromObject(route).String()
		resource := newUnstructuredResource("orphaned", "default", routeKey+",other-ns/other-route")
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		skipDelete, err := converter.HandleOrphanedResource(context.Background(), logr.Discard(), resource)
		require.NoError(t, err)
		assert.True(t, skipDelete)
		assert.Equal(t, "other-ns/other-route", resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesAnnotation])
	})

	t.Run("allows deletion when only route remains", func(t *testing.T) {
		routeKey := client.ObjectKeyFromObject(route).String()
		resource := newUnstructuredResource("orphaned", "default", routeKey)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(resource).Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		skipDelete, err := converter.HandleOrphanedResource(context.Background(), logr.Discard(), resource)
		require.NoError(t, err)
		assert.False(t, skipDelete)
		_, exists := resource.GetAnnotations()[consts.GatewayOperatorHybridRoutesAnnotation]
		assert.False(t, exists)
	})

	t.Run("returns error when patch fails", func(t *testing.T) {
		routeKey := client.ObjectKeyFromObject(route).String()
		resource := newUnstructuredResource("orphaned", "default", routeKey+",other-ns/other-route")
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(resource).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return fmt.Errorf("simulated patch error")
				},
			}).
			Build()
		converter := newHTTPRouteConverter(route, fakeClient, false, "").(*httpRouteConverter)

		skipDelete, err := converter.HandleOrphanedResource(context.Background(), logr.Discard(), resource)
		require.Error(t, err)
		assert.True(t, skipDelete)
		assert.Contains(t, err.Error(), "failed to update resource")
	})
}

func TestHTTPRouteConverter_GetHostnamesByParentRef(t *testing.T) {
	ctx := context.Background()

	t.Run("returns nil when section name mismatches", func(t *testing.T) {
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

		hostnames, err := converter.getHostnamesByParentRef(ctx, logr.Discard(), pRef)
		require.NoError(t, err)
		assert.Nil(t, hostnames)
	})

	t.Run("returns hostnames when section matches", func(t *testing.T) {
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

		hostnames, err := converter.getHostnamesByParentRef(ctx, logr.Discard(), pRef)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"api.example.com", "web.example.com"}, hostnames)
	})
}

func TestHTTPRouteConverter_MetadataAccessors(t *testing.T) {
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

func newHTTPRouteForTranslation(namespace string, hostnames []string, backendRefs []gwtypes.HTTPBackendRef, filters []gwtypes.HTTPRouteFilter) *gwtypes.HTTPRoute {
	var gwHostnames []gatewayv1.Hostname
	for _, hostname := range hostnames {
		gwHostnames = append(gwHostnames, gatewayv1.Hostname(hostname))
	}

	return &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: namespace,
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

func newBackendRef(name, namespace string, port int32) gwtypes.HTTPBackendRef {
	serviceKind := gwtypes.Kind("Service")
	serviceGroup := gwtypes.Group("")
	ref := gwtypes.HTTPBackendRef{
		BackendRef: gwtypes.BackendRef{
			BackendObjectReference: gwtypes.BackendObjectReference{
				Name:  gwtypes.ObjectName(name),
				Kind:  &serviceKind,
				Group: &serviceGroup,
				Port:  lo.ToPtr(gwtypes.PortNumber(port)),
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

func newService(name, namespace string, port, targetPort int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(int(targetPort)),
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

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newUnstructuredResource(name, namespace, routesAnnotation string) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongService"))
	resource.SetName(name)
	resource.SetNamespace(namespace)
	if routesAnnotation != "" {
		resource.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridRoutesAnnotation: routesAnnotation,
		})
	}
	return resource
}
