package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

func Test_listGRPCRoutesForGateway_table(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.GRPCRoute{}, &gwtypes.Gateway{},
	)
	_ = gatewayv1.Install(scheme)

	gateway := &gwtypes.Gateway{
		Namespace: "test-ns",
		Name:      "test-gw",
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}

	grpcRoute := &gwtypes.GRPCRoute{
		Namespace: "test-ns",
		Name:      "route-1",
		Spec: gwtypes.GRPCRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name: gwtypes.ObjectName("test-gw"),
				}},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gateway, grpcRoute).
		WithIndex(&gwtypes.GRPCRoute{}, index.GatewayOnGRPCRouteIndex, index.GatewaysOnRoute[gwtypes.GRPCRoute]).
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
			requests, err := listGRPCRoutesForGateway(ctx, tt.client, "test-ns", "test-gw")
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

func Test_listGRPCRoutesForService(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.GRPCRoute{}, &corev1.Service{},
	)
	_ = gatewayv1.Install(scheme)

	svc := &corev1.Service{
		Namespace: "test-ns",
		Name:      "test-svc",
	}

	grpcRoute := &gwtypes.GRPCRoute{
		Namespace: "test-ns",
		Name:      "route-1",
		Spec: gwtypes.GRPCRouteSpec{
			Rules: []gwtypes.GRPCRouteRule{{
				BackendRefs: []gwtypes.GRPCBackendRef{{
					Name: gatewayv1.ObjectName("test-svc"),
				}},
			}},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, grpcRoute).
		WithIndex(&gwtypes.GRPCRoute{}, index.BackendServicesOnGRPCRouteIndex, func(obj client.Object) []string {
			grpcRoute, ok := obj.(*gwtypes.GRPCRoute)
			if !ok {
				return nil
			}
			var keys []string
			for _, rule := range grpcRoute.Spec.Rules {
				for _, ref := range rule.BackendRefs {
					keys = append(keys, grpcRoute.Namespace+"/"+string(ref.BackendRef.Name))
				}
			}
			return keys
		}).
		Build()

	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		requests, err := listGRPCRoutesForService(ctx, cl, "test-ns", "test-svc")
		require.NoError(t, err)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "test-ns", requests[0].Namespace)
	})

	t.Run("error branch", func(t *testing.T) {
		requests, err := listGRPCRoutesForService(context.Background(), &fakeErrorClient{}, "test-ns", "test-svc")
		require.Error(t, err)
		require.Nil(t, requests)
	})
}

func Test_MapGRPCRouteForReferenceGrant(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.GRPCRoute{}, &gwtypes.ReferenceGrant{},
	)
	_ = gatewayv1.Install(scheme)

	// GRPCRoute in source-ns that references a service in target-ns.
	grpcRouteWithCrossNsRef := &gwtypes.GRPCRoute{
		Namespace: "source-ns",
		Name:      "route-1",
		Spec: gwtypes.GRPCRouteSpec{
			Rules: []gwtypes.GRPCRouteRule{{
				BackendRefs: []gwtypes.GRPCBackendRef{{
					Name:      gatewayv1.ObjectName("test-svc"),
					Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("target-ns"); return &ns }(),
				}},
			}},
		},
	}

	// GRPCRoute in source-ns that only references same-namespace services.
	grpcRouteSameNs := &gwtypes.GRPCRoute{
		Namespace: "source-ns",
		Name:      "route-2",
		Spec: gwtypes.GRPCRouteSpec{
			Rules: []gwtypes.GRPCRouteRule{{
				BackendRefs: []gwtypes.GRPCBackendRef{{
					Name: gatewayv1.ObjectName("local-svc"),
				}},
			}},
		},
	}

	// ReferenceGrant that allows GRPCRoutes from source-ns to reference resources in target-ns.
	referenceGrant := &gwtypes.ReferenceGrant{
		Namespace: "target-ns",
		Name:      "test-grant",
		Spec: gwtypes.ReferenceGrantSpec{
			From: []gwtypes.ReferenceGrantFrom{{
				Group:     gwtypes.GroupName,
				Kind:      "GRPCRoute",
				Namespace: "source-ns",
			}},
			To: []gwtypes.ReferenceGrantTo{{
				Group: "",
				Kind:  "Service",
			}},
		},
	}

	t.Run("success - finds GRPCRoute with cross-namespace ref", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(referenceGrant, grpcRouteWithCrossNsRef, grpcRouteSameNs).
			Build()

		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)

		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
		require.Equal(t, "source-ns", requests[0].Namespace)
	})

	t.Run("wrong type", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		obj := &gwtypes.Gateway{}
		requests := mapFunc(ctx, obj)
		require.Nil(t, requests)
	})

	t.Run("skip non-GRPCRoute kind", func(t *testing.T) {
		rgWithWrongKind := &gwtypes.ReferenceGrant{
			Namespace: "target-ns",
			Name:      "wrong-kind-grant",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{{
					Group: gwtypes.GroupName,
					// Not GRPCRoute.
					Kind:      "TCPRoute",
					Namespace: "source-ns",
				}},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgWithWrongKind, grpcRouteWithCrossNsRef).
			Build()

		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgWithWrongKind)
		require.Empty(t, requests)
	})

	t.Run("skip wrong group", func(t *testing.T) {
		rgWithWrongGroup := &gwtypes.ReferenceGrant{
			Namespace: "target-ns",
			Name:      "wrong-group-grant",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{{
					Group:     "some.other.group",
					Kind:      "GRPCRoute",
					Namespace: "source-ns",
				}},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgWithWrongGroup, grpcRouteWithCrossNsRef).
			Build()

		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgWithWrongGroup)
		require.Empty(t, requests)
	})

	t.Run("accept empty group", func(t *testing.T) {
		rgWithEmptyGroup := &gwtypes.ReferenceGrant{
			Namespace: "target-ns",
			Name:      "empty-group-grant",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{{
					// Empty group should be accepted.
					Group:     "",
					Kind:      "GRPCRoute",
					Namespace: "source-ns",
				}},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgWithEmptyGroup, grpcRouteWithCrossNsRef).
			Build()

		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgWithEmptyGroup)
		require.Len(t, requests, 1)
		require.Equal(t, "route-1", requests[0].Name)
	})

	t.Run("no cross-namespace refs", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(referenceGrant, grpcRouteSameNs).
			Build()

		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)
		require.Empty(t, requests)
	})

	t.Run("error listing GRPCRoutes", func(t *testing.T) {
		mapFunc := MapGRPCRouteForReferenceGrant(&fakeErrorClient{})
		ctx := context.Background()
		requests := mapFunc(ctx, referenceGrant)
		require.Nil(t, requests)
	})

	t.Run("multiple from clauses", func(t *testing.T) {
		grpcRouteOtherNs := &gwtypes.GRPCRoute{
			Namespace: "other-ns",
			Name:      "route-3",
			Spec: gwtypes.GRPCRouteSpec{
				Rules: []gwtypes.GRPCRouteRule{{
					BackendRefs: []gwtypes.GRPCBackendRef{{
						Name:      gatewayv1.ObjectName("test-svc"),
						Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("target-ns"); return &ns }(),
					}},
				}},
			},
		}

		rgMultipleFrom := &gwtypes.ReferenceGrant{
			Namespace: "target-ns",
			Name:      "multi-grant",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{
					{
						Group:     gwtypes.GroupName,
						Kind:      "GRPCRoute",
						Namespace: "source-ns",
					},
					{
						Group:     gwtypes.GroupName,
						Kind:      "GRPCRoute",
						Namespace: "other-ns",
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgMultipleFrom, grpcRouteWithCrossNsRef, grpcRouteOtherNs).
			Build()

		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgMultipleFrom)
		require.Len(t, requests, 2)
		names := []string{requests[0].Name, requests[1].Name}
		assert.Contains(t, names, "route-1")
		assert.Contains(t, names, "route-3")
	})

	t.Run("empty from list", func(t *testing.T) {
		rgEmptyFrom := &gwtypes.ReferenceGrant{
			Namespace: "target-ns",
			Name:      "empty-from-grant",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(rgEmptyFrom, grpcRouteWithCrossNsRef).
			Build()

		mapFunc := MapGRPCRouteForReferenceGrant(cl)
		ctx := context.Background()
		requests := mapFunc(ctx, rgEmptyFrom)
		require.Empty(t, requests)
	})
}

func Test_MapGRPCRouteForKongPlugin(t *testing.T) {
	const hybridRoutesAnnotation = "gateway-operator.konghq.com/hybrid-routes-GRPCRoute"

	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.GRPCRoute{},
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
				Namespace: "test-ns",
				Name:      "test-svc",
			},
			objects:   []client.Object{},
			expectNil: true,
		},
		{
			name: "plugin referenced via extensionRef filter",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					Namespace:  "test-ns",
					Name:       "test-plugin",
					PluginName: "rate-limiting",
				},
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-with-filter",
					Spec: gwtypes.GRPCRouteSpec{
						Rules: []gwtypes.GRPCRouteRule{
							{
								Filters: []gatewayv1.GRPCRouteFilter{
									{
										Type: gatewayv1.GRPCRouteFilterExtensionRef,
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
				Namespace:  "test-ns",
				Name:       "test-plugin",
				PluginName: "rate-limiting",
			},
			expectedCount: 1,
			expectedNames: []string{"route-with-filter"},
		},
		{
			name: "plugin referenced via annotation",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					Namespace: "test-ns",
					Name:      "plugin-with-annotation",
					Annotations: map[string]string{
						hybridRoutesAnnotation: "test-ns/route-with-annotation",
					},
					PluginName: "rate-limiting",
				},
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-with-annotation",
				},
			},
			inputObject: &configurationv1.KongPlugin{
				Namespace: "test-ns",
				Name:      "plugin-with-annotation",
				Annotations: map[string]string{
					hybridRoutesAnnotation: "test-ns/route-with-annotation",
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
					Namespace: "test-ns",
					Name:      "plugin-both",
					Annotations: map[string]string{
						hybridRoutesAnnotation: "test-ns/route-with-annotation",
					},
					PluginName: "rate-limiting",
				},
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-with-filter",
					Spec: gwtypes.GRPCRouteSpec{
						Rules: []gwtypes.GRPCRouteRule{
							{
								Filters: []gatewayv1.GRPCRouteFilter{
									{
										Type: gatewayv1.GRPCRouteFilterExtensionRef,
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
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-with-annotation",
				},
			},
			inputObject: &configurationv1.KongPlugin{
				Namespace: "test-ns",
				Name:      "plugin-both",
				Annotations: map[string]string{
					hybridRoutesAnnotation: "test-ns/route-with-annotation",
				},
				PluginName: "rate-limiting",
			},
			expectedCount: 2,
			expectedNames: []string{"route-with-filter", "route-with-annotation"},
		},
		{
			name: "multiple GRPCRoutes with filter referencing the same plugin",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					Namespace:  "test-ns",
					Name:       "shared-plugin",
					PluginName: "cors",
				},
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-1",
					Spec: gwtypes.GRPCRouteSpec{
						Rules: []gwtypes.GRPCRouteRule{
							{
								Filters: []gatewayv1.GRPCRouteFilter{
									{
										Type: gatewayv1.GRPCRouteFilterExtensionRef,
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
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-2",
					Spec: gwtypes.GRPCRouteSpec{
						Rules: []gwtypes.GRPCRouteRule{
							{
								Filters: []gatewayv1.GRPCRouteFilter{
									{
										Type: gatewayv1.GRPCRouteFilterExtensionRef,
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
				Namespace:  "test-ns",
				Name:       "shared-plugin",
				PluginName: "cors",
			},
			expectedCount: 2,
			expectedNames: []string{"route-1", "route-2"},
		},
		{
			name: "no GRPCRoutes reference the plugin",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					Namespace:  "test-ns",
					Name:       "test-plugin",
					PluginName: "rate-limiting",
				},
			},
			inputObject: &configurationv1.KongPlugin{
				Namespace:  "test-ns",
				Name:       "test-plugin",
				PluginName: "rate-limiting",
			},
			expectEmpty: true,
		},
		{
			name: "list error returns nil",
			inputObject: &configurationv1.KongPlugin{
				Namespace:  "test-ns",
				Name:       "test-plugin",
				PluginName: "rate-limiting",
			},
			client:    &fakeErrorClient{},
			expectNil: true,
		},
		{
			name: "multiple annotations with multiple routes",
			objects: []client.Object{
				&configurationv1.KongPlugin{
					Namespace: "test-ns",
					Name:      "plugin-multi",
					Annotations: map[string]string{
						hybridRoutesAnnotation: "test-ns/route-1,test-ns/route-2",
					},
					PluginName: "rate-limiting",
				},
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-1",
				},
				&gwtypes.GRPCRoute{
					Namespace: "test-ns",
					Name:      "route-2",
				},
			},
			inputObject: &configurationv1.KongPlugin{
				Namespace: "test-ns",
				Name:      "plugin-multi",
				Annotations: map[string]string{
					hybridRoutesAnnotation: "test-ns/route-1,test-ns/route-2",
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
					WithIndex(&gwtypes.GRPCRoute{}, index.KongPluginsOnGRPCRouteIndex, index.KongPluginsOnGRPCRoute).
					Build()
			}

			mapFunc := MapGRPCRouteForKongPlugin(cl)
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
