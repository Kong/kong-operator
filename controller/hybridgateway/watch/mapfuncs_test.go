package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
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
