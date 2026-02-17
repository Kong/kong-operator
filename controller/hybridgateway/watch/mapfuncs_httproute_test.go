package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// partialErrorClient simulates a client.Client that returns an error only when listing HTTPRoutes for a Gateway.
type partialErrorClient struct {
	client.Client

	gateways *gwtypes.GatewayList
}

func (f *partialErrorClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	switch o := obj.(type) {
	case *gwtypes.GatewayList:
		*o = *f.gateways
		return nil
	case *gwtypes.HTTPRouteList:
		return assert.AnError
	default:
		return nil
	}
}

// fakeErrorClient simulates a client.Client that always returns an error on List.
type fakeErrorClient struct {
	client.Client
}

func (f *fakeErrorClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	return assert.AnError
}

// getErrorClient simulates a client.Client that always returns an error on Get.
type getErrorClient struct {
	client.Client
}

func (c *getErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return assert.AnError
}

// listErrorClient simulates a client.Client that returns an error only when listing HTTPRoutes for a Service.
type listErrorClient struct {
	client.Client
}

func (c *listErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return nil // Service fetch succeeds
}

func (c *listErrorClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	if _, ok := obj.(*gwtypes.HTTPRouteList); ok {
		return assert.AnError
	}
	return nil
}

func Test_listHTTPRoutesForGateway_table(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{}, &gwtypes.Gateway{},
	)
	_ = gatewayv1.Install(scheme)

	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-gw",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.HTTPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name: gwtypes.ObjectName("test-gw"),
				}},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gateway, httpRoute).
		WithIndex(&gwtypes.HTTPRoute{}, index.GatewayOnHTTPRouteIndex, index.GatewaysOnHTTPRoute).
		Build()

	tests := []struct {
		name      string
		client    client.Client
		wantErr   bool
		wantCount int
	}{
		{
			name:      "success",
			client:    cl,
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:      "error branch",
			client:    &fakeErrorClient{},
			wantErr:   true,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			requests, err := listHTTPRoutesForGateway(ctx, tt.client, "test-ns", "test-gw")
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, requests)
			} else {
				require.NoError(t, err)
				require.Len(t, requests, tt.wantCount)
				if tt.wantCount > 0 {
					require.Equal(t, "route-1", requests[0].Name)
					require.Equal(t, "test-ns", requests[0].Namespace)
				}
			}
		})
	}
}

func Test_MapHTTPRouteForGateway(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{}, &gwtypes.Gateway{},
	)
	_ = gatewayv1.Install(scheme)

	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-gw",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.HTTPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name: gwtypes.ObjectName("test-gw"),
				}},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gateway, httpRoute).
		WithIndex(&gwtypes.HTTPRoute{}, index.GatewayOnHTTPRouteIndex, index.GatewaysOnHTTPRoute).
		Build()

	mapFunc := MapHTTPRouteForGateway(cl)

	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		obj := gateway
		requests := mapFunc(ctx, obj)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "test-ns", requests[0].Namespace)
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.Background()
		obj := &gwtypes.GatewayClass{}
		requests := mapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("error branch", func(t *testing.T) {
		// Use a fake client that always errors.
		errorMapFunc := MapHTTPRouteForGateway(&fakeErrorClient{})
		ctx := context.Background()
		obj := gateway
		requests := errorMapFunc(ctx, obj)
		require.Nil(t, requests)
	})
}

func Test_MapHTTPRouteForGatewayClass(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{}, &gwtypes.Gateway{}, &gwtypes.GatewayClass{},
	)
	_ = gatewayv1.Install(scheme)

	gatewayClass := &gwtypes.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-class",
		},
	}

	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-gw",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.HTTPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name: gwtypes.ObjectName("test-gw"),
				}},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gatewayClass, gateway, httpRoute).
		WithIndex(&gwtypes.Gateway{}, index.GatewayClassOnGatewayIndex, index.GatewayClassOnGateway).
		WithIndex(&gwtypes.HTTPRoute{}, index.GatewayOnHTTPRouteIndex, index.GatewaysOnHTTPRoute).
		Build()

	mapFunc := MapHTTPRouteForGatewayClass(cl)

	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		obj := gatewayClass
		requests := mapFunc(ctx, obj)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "test-ns", requests[0].Namespace)
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.Background()
		obj := &gwtypes.Gateway{}
		requests := mapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("error branch - gatewayclass list", func(t *testing.T) {
		errorMapFunc := MapHTTPRouteForGatewayClass(&fakeErrorClient{})
		ctx := context.Background()
		obj := gatewayClass
		requests := errorMapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("error branch - httproute list in loop", func(t *testing.T) {
		gateways := &gwtypes.GatewayList{Items: []gwtypes.Gateway{*gateway}}
		cl := &partialErrorClient{gateways: gateways}
		errorMapFunc := MapHTTPRouteForGatewayClass(cl)
		ctx := context.Background()
		obj := gatewayClass
		requests := errorMapFunc(ctx, obj)
		require.Nil(t, requests)
	})
}

func Test_listHTTPRoutesForService(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{}, &corev1.Service{},
	)
	_ = gatewayv1.Install(scheme)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-svc",
		},
	}

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					BackendRef: gwtypes.BackendRef{
						BackendObjectReference: gwtypes.BackendObjectReference{
							Name: gatewayv1.ObjectName("test-svc"),
						},
					},
				}},
			}},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, httpRoute).
		WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, func(obj client.Object) []string {
			httpRoute, ok := obj.(*gwtypes.HTTPRoute)
			if !ok {
				return nil
			}
			var keys []string
			for _, rule := range httpRoute.Spec.Rules {
				for _, ref := range rule.BackendRefs {
					keys = append(keys, httpRoute.Namespace+"/"+string(ref.BackendRef.Name))
				}
			}
			return keys
		}).
		Build()

	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		requests, err := listHTTPRoutesForService(ctx, cl, "test-ns", "test-svc")
		require.NoError(t, err)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "test-ns", requests[0].Namespace)
	})

	t.Run("error branch", func(t *testing.T) {
		requests, err := listHTTPRoutesForService(context.Background(), &fakeErrorClient{}, "test-ns", "test-svc")
		require.Error(t, err)
		require.Nil(t, requests)
	})

	t.Run("error branch - list error", func(t *testing.T) {
		requests, err := listHTTPRoutesForService(context.Background(), &listErrorClient{}, "test-ns", "test-svc")
		require.Error(t, err)
		require.Nil(t, requests)
	})
}

func Test_MapHTTPRouteForService(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{}, &corev1.Service{},
	)
	_ = gatewayv1.Install(scheme)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-svc",
		},
	}

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					BackendRef: gwtypes.BackendRef{
						BackendObjectReference: gwtypes.BackendObjectReference{
							Name: gatewayv1.ObjectName("test-svc"),
						},
					},
				}},
			}},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, httpRoute).
		WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, func(obj client.Object) []string {
			httpRoute, ok := obj.(*gwtypes.HTTPRoute)
			if !ok {
				return nil
			}
			var keys []string
			for _, rule := range httpRoute.Spec.Rules {
				for _, ref := range rule.BackendRefs {
					keys = append(keys, httpRoute.Namespace+"/"+string(ref.BackendRef.Name))
				}
			}
			return keys
		}).
		Build()

	mapFunc := MapHTTPRouteForService(cl)

	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		obj := svc
		requests := mapFunc(ctx, obj)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "test-ns", requests[0].Namespace)
	})

	t.Run("service in different namespace", func(t *testing.T) {
		// Service in 'other-ns', HTTPRoute in 'test-ns' referencing 'test-svc'
		otherSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "other-ns",
				Name:      "test-svc",
			},
		}
		clDiffNS := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(otherSvc, httpRoute).
			WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, func(obj client.Object) []string {
				httpRoute, ok := obj.(*gwtypes.HTTPRoute)
				if !ok {
					return nil
				}
				var keys []string
				for _, rule := range httpRoute.Spec.Rules {
					for _, ref := range rule.BackendRefs {
						keys = append(keys, httpRoute.Namespace+"/"+string(ref.BackendRef.Name))
					}
				}
				return keys
			}).
			Build()
		mapFuncDiffNS := MapHTTPRouteForService(clDiffNS)
		ctx := context.Background()
		obj := otherSvc
		requests := mapFuncDiffNS(ctx, obj)
		require.Empty(t, requests)
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.Background()
		obj := &corev1.Pod{}
		requests := mapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("error branch", func(t *testing.T) {
		errorMapFunc := MapHTTPRouteForService(&fakeErrorClient{})
		ctx := context.Background()
		obj := svc
		requests := errorMapFunc(ctx, obj)
		require.Nil(t, requests)
	})
}

func Test_MapHTTPRouteForEndpointSlice(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{}, &corev1.Service{}, &discoveryv1.EndpointSlice{},
	)
	_ = gatewayv1.Install(scheme)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-svc",
		},
	}

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					BackendRef: gwtypes.BackendRef{
						BackendObjectReference: gwtypes.BackendObjectReference{
							Name: gatewayv1.ObjectName("test-svc"),
						},
					},
				}},
			}},
		},
	}

	epSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "slice-1",
			Labels: map[string]string{
				discoveryv1.LabelServiceName: "test-svc",
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, httpRoute, epSlice).
		WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, func(obj client.Object) []string {
			httpRoute, ok := obj.(*gwtypes.HTTPRoute)
			if !ok {
				return nil
			}
			var keys []string
			for _, rule := range httpRoute.Spec.Rules {
				for _, ref := range rule.BackendRefs {
					keys = append(keys, httpRoute.Namespace+"/"+string(ref.BackendRef.Name))
				}
			}
			return keys
		}).
		Build()

	mapFunc := MapHTTPRouteForEndpointSlice(cl)

	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		obj := epSlice
		requests := mapFunc(ctx, obj)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "test-ns", requests[0].Namespace)
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.Background()
		obj := &corev1.Pod{}
		requests := mapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("missing service label", func(t *testing.T) {
		ctx := context.Background()
		badSlice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "slice-2",
			},
		}
		requests := mapFunc(ctx, badSlice)
		require.Nil(t, requests)
	})

	t.Run("service not found", func(t *testing.T) {
		ctx := context.Background()
		missingSvcSlice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "slice-3",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "missing-svc",
				},
			},
		}
		requests := mapFunc(ctx, missingSvcSlice)
		require.Nil(t, requests)
	})

	t.Run("error branch", func(t *testing.T) {
		clGetErr := &getErrorClient{}
		errorMapFunc := MapHTTPRouteForEndpointSlice(clGetErr)
		ctx := context.Background()
		obj := epSlice
		requests := errorMapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("error on HTTPRoute list", func(t *testing.T) {
		clListErr := &listErrorClient{}
		mapFuncErrList := MapHTTPRouteForEndpointSlice(clListErr)
		ctx := context.Background()
		obj := epSlice
		requests := mapFuncErrList(ctx, obj)
		require.Nil(t, requests)
	})
}

func Test_MapHTTPRouteForReferenceGrant(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{}, &gwtypes.ReferenceGrant{},
	)
	_ = gatewayv1.Install(scheme)

	// HTTPRoute in source-ns that references a service in target-ns.
	httpRouteWithCrossNsRef := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "source-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					BackendRef: gwtypes.BackendRef{
						BackendObjectReference: gwtypes.BackendObjectReference{
							Name:      gatewayv1.ObjectName("test-svc"),
							Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("target-ns"); return &ns }(),
						},
					},
				}},
			}},
		},
	}

	// HTTPRoute in source-ns that only references same-namespace services.
	httpRouteSameNs := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "source-ns",
			Name:      "route-2",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					BackendRef: gwtypes.BackendRef{
						BackendObjectReference: gwtypes.BackendObjectReference{
							Name: gatewayv1.ObjectName("local-svc"),
						},
					},
				}},
			}},
		},
	}

	// ReferenceGrant that allows HTTPRoutes from source-ns to reference resources in target-ns.
	referenceGrant := &gwtypes.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "target-ns",
			Name:      "test-grant",
		},
		Spec: gwtypes.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{{
				Group:     gwtypes.GroupName,
				Kind:      "HTTPRoute",
				Namespace: "source-ns",
			}},
			To: []gatewayv1beta1.ReferenceGrantTo{{
				Group: "",
				Kind:  "Service",
			}},
		},
	}

	t.Run("success - finds HTTPRoute with cross-namespace ref", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(referenceGrant, httpRouteWithCrossNsRef, httpRouteSameNs).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)

		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "source-ns", requests[0].Namespace)
	})

	t.Run("wrong type", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		obj := &gwtypes.Gateway{}
		requests := mapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("skip non-HTTPRoute kind", func(t *testing.T) {
		rgWithWrongKind := &gwtypes.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "target-ns",
				Name:      "wrong-kind-grant",
			},
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group: gwtypes.GroupName,
					// Not HTTPRoute.
					Kind:      "TCPRoute",
					Namespace: "source-ns",
				}},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgWithWrongKind, httpRouteWithCrossNsRef).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgWithWrongKind)
		require.Empty(t, requests)
	})

	t.Run("skip wrong group", func(t *testing.T) {
		rgWithWrongGroup := &gwtypes.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "target-ns",
				Name:      "wrong-group-grant",
			},
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     "some.other.group",
					Kind:      "HTTPRoute",
					Namespace: "source-ns",
				}},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgWithWrongGroup, httpRouteWithCrossNsRef).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgWithWrongGroup)
		require.Empty(t, requests)
	})

	t.Run("accept empty group", func(t *testing.T) {
		rgWithEmptyGroup := &gwtypes.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "target-ns",
				Name:      "empty-group-grant",
			},
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					// Empty group should be accepted.
					Group:     "",
					Kind:      "HTTPRoute",
					Namespace: "source-ns",
				}},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgWithEmptyGroup, httpRouteWithCrossNsRef).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgWithEmptyGroup)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
	})

	t.Run("no cross-namespace refs", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(referenceGrant, httpRouteSameNs).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)
		require.Empty(t, requests)
	})

	t.Run("error listing HTTPRoutes", func(t *testing.T) {
		mapFunc := MapHTTPRouteForReferenceGrant(&fakeErrorClient{})
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)
		require.Nil(t, requests)
	})

	t.Run("multiple from clauses", func(t *testing.T) {
		httpRouteOtherNs := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "other-ns",
				Name:      "route-3",
			},
			Spec: gwtypes.HTTPRouteSpec{
				Rules: []gwtypes.HTTPRouteRule{{
					BackendRefs: []gwtypes.HTTPBackendRef{{
						BackendRef: gwtypes.BackendRef{
							BackendObjectReference: gwtypes.BackendObjectReference{
								Name:      gatewayv1.ObjectName("test-svc"),
								Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("target-ns"); return &ns }(),
							},
						},
					}},
				}},
			},
		}

		rgMultipleFrom := &gwtypes.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "target-ns",
				Name:      "multi-grant",
			},
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{
					{
						Group:     gwtypes.GroupName,
						Kind:      "HTTPRoute",
						Namespace: "source-ns",
					},
					{
						Group:     gwtypes.GroupName,
						Kind:      "HTTPRoute",
						Namespace: "other-ns",
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgMultipleFrom, httpRouteWithCrossNsRef, httpRouteOtherNs).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgMultipleFrom)
		require.Len(t, requests, 2)
		names := []string{requests[0].Name, requests[1].Name}
		assert.Contains(t, names, "route-1")
		assert.Contains(t, names, "route-3")
	})

	t.Run("multiple rules with mixed refs", func(t *testing.T) {
		httpRouteMultiRules := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "source-ns",
				Name:      "route-multi",
			},
			Spec: gwtypes.HTTPRouteSpec{
				Rules: []gwtypes.HTTPRouteRule{
					{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name: gatewayv1.ObjectName("local-svc"),
								},
							},
						}},
					},
					{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:      gatewayv1.ObjectName("remote-svc"),
									Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("target-ns"); return &ns }(),
								},
							},
						}},
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(referenceGrant, httpRouteMultiRules).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)
		require.Len(t, requests, 1)
		require.Equal(t, "route-multi", requests[0].Name)
	})

	t.Run("empty from list", func(t *testing.T) {
		rgEmptyFrom := &gwtypes.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "target-ns",
				Name:      "empty-from-grant",
			},
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgEmptyFrom, httpRouteWithCrossNsRef).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgEmptyFrom)
		require.Empty(t, requests)
	})

	t.Run("multiple routes in from namespace but only cross-namespace ref returned", func(t *testing.T) {
		// This HTTPRoute in source-ns references a different namespace (not target-ns).
		httpRouteOtherTarget := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "source-ns",
				Name:      "route-other",
			},
			Spec: gwtypes.HTTPRouteSpec{
				Rules: []gwtypes.HTTPRouteRule{{
					BackendRefs: []gwtypes.HTTPBackendRef{{
						BackendRef: gwtypes.BackendRef{
							BackendObjectReference: gwtypes.BackendObjectReference{
								Name:      gatewayv1.ObjectName("other-svc"),
								Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("other-target-ns"); return &ns }(),
							},
						},
					}},
				}},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(referenceGrant, httpRouteWithCrossNsRef, httpRouteSameNs, httpRouteOtherTarget).
			Build()

		mapFunc := MapHTTPRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)

		// Only route-1 should be returned (references target-ns).
		// route-2 only references same namespace.
		// route-other references a different target namespace.
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "source-ns", requests[0].Namespace)
	})
}

func Test_MapHTTPRouteForKongPlugin(t *testing.T) {
	const hybridRoutesAnnotation = "gateway-operator.konghq.com/hybrid-routes"

	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.HTTPRoute{},
	)
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: configurationv1.GroupVersion.Group, Version: configurationv1.GroupVersion.Version},
		&configurationv1.KongPlugin{},
	)
	_ = gatewayv1.Install(scheme)
	_ = configurationv1.AddToScheme(scheme)

	testCases := []struct {
		name          string
		objects       []client.Object
		inputObject   client.Object
		client        client.Client
		expectedCount int
		expectedNames []string
		expectNil     bool
		expectEmpty   bool
	}{
		{
			name: "wrong object type returns nil",
			inputObject: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test-svc",
				},
			},
			objects:   []client.Object{},
			expectNil: true,
		},
		{
			name: "plugin referenced via extensionRef filter",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "test-plugin",
					},
					PluginName: "rate-limiting",
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-with-filter",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								Filters: []gwtypes.HTTPRouteFilter{
									{
										Type: gatewayv1.HTTPRouteFilterExtensionRef,
										ExtensionRef: &gatewayv1.LocalObjectReference{
											Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
											Kind:  "KongPlugin",
											Name:  "test-plugin",
										},
									},
								},
							},
						},
					},
				},
			},
			inputObject: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test-plugin",
				},
				PluginName: "rate-limiting",
			},
			expectedCount: 1,
			expectedNames: []string{"route-with-filter"},
		},
		{
			name: "plugin referenced via annotation",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "plugin-with-annotation",
						Annotations: map[string]string{
							hybridRoutesAnnotation: "test-ns/route-with-annotation",
						},
					},
					PluginName: "rate-limiting",
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-with-annotation",
					},
				},
			},
			inputObject: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "plugin-with-annotation",
					Annotations: map[string]string{
						hybridRoutesAnnotation: "test-ns/route-with-annotation",
					},
				},
				PluginName: "rate-limiting",
			},
			expectedCount: 1,
			expectedNames: []string{"route-with-annotation"},
		},
		{
			name: "plugin referenced via both filter and annotation",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "plugin-both",
						Annotations: map[string]string{
							hybridRoutesAnnotation: "test-ns/route-with-annotation",
						},
					},
					PluginName: "rate-limiting",
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-with-filter",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								Filters: []gwtypes.HTTPRouteFilter{
									{
										Type: gatewayv1.HTTPRouteFilterExtensionRef,
										ExtensionRef: &gatewayv1.LocalObjectReference{
											Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
											Kind:  "KongPlugin",
											Name:  "plugin-both",
										},
									},
								},
							},
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-with-annotation",
					},
				},
			},
			inputObject: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "plugin-both",
					Annotations: map[string]string{
						hybridRoutesAnnotation: "test-ns/route-with-annotation",
					},
				},
				PluginName: "rate-limiting",
			},
			expectedCount: 2,
			expectedNames: []string{"route-with-filter", "route-with-annotation"},
		},
		{
			name: "multiple HTTPRoutes with filter referencing the same plugin",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "shared-plugin",
					},
					PluginName: "cors",
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-1",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								Filters: []gwtypes.HTTPRouteFilter{
									{
										Type: gatewayv1.HTTPRouteFilterExtensionRef,
										ExtensionRef: &gatewayv1.LocalObjectReference{
											Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
											Kind:  "KongPlugin",
											Name:  "shared-plugin",
										},
									},
								},
							},
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-2",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								Filters: []gwtypes.HTTPRouteFilter{
									{
										Type: gatewayv1.HTTPRouteFilterExtensionRef,
										ExtensionRef: &gatewayv1.LocalObjectReference{
											Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
											Kind:  "KongPlugin",
											Name:  "shared-plugin",
										},
									},
								},
							},
						},
					},
				},
			},
			inputObject: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "shared-plugin",
				},
				PluginName: "cors",
			},
			expectedCount: 2,
			expectedNames: []string{"route-1", "route-2"},
		},
		{
			name: "no HTTPRoutes reference the plugin",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "test-plugin",
					},
					PluginName: "rate-limiting",
				},
			},
			inputObject: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test-plugin",
				},
				PluginName: "rate-limiting",
			},
			expectEmpty: true,
		},
		{
			name: "list error returns nil",
			inputObject: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test-plugin",
				},
				PluginName: "rate-limiting",
			},
			client:    &fakeErrorClient{},
			expectNil: true,
		},
		{
			name: "multiple annotations with multiple routes",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "plugin-multi",
						Annotations: map[string]string{
							hybridRoutesAnnotation: "test-ns/route-1,test-ns/route-2",
						},
					},
					PluginName: "rate-limiting",
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-1",
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "route-2",
					},
				},
			},
			inputObject: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "plugin-multi",
					Annotations: map[string]string{
						hybridRoutesAnnotation: "test-ns/route-1,test-ns/route-2",
					},
				},
				PluginName: "rate-limiting",
			},
			expectedCount: 2,
			expectedNames: []string{"route-1", "route-2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var cl client.Client
			if tc.client != nil {
				cl = tc.client
			} else {
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tc.objects...).
					WithIndex(&gwtypes.HTTPRoute{}, index.KongPluginsOnHTTPRouteIndex, index.KongPluginsOnHTTPRoute).
					Build()
			}

			mapFunc := MapHTTPRouteForKongPlugin(cl)
			ctx := context.Background()
			requests := mapFunc(ctx, tc.inputObject)

			if tc.expectNil {
				require.Nil(t, requests)
				return
			}

			if tc.expectEmpty {
				require.Empty(t, requests)
				return
			}

			require.Len(t, requests, tc.expectedCount)
			if len(tc.expectedNames) > 0 {
				actualNames := make([]string, len(requests))
				for i, req := range requests {
					actualNames[i] = req.Name
				}
				for _, expectedName := range tc.expectedNames {
					require.Contains(t, actualNames, expectedName)
				}
			}
		})
	}
}
