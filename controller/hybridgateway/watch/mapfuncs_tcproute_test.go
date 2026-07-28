package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestMapRouteForTCPRoutePlumbing(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
		&gwtypes.TCPRoute{}, &gwtypes.TCPRouteList{}, &gwtypes.Gateway{}, &gwtypes.GatewayList{}, &gwtypes.GatewayClass{},
	)
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Service{}, &corev1.ServiceList{})
	scheme.AddKnownTypes(discoveryv1.SchemeGroupVersion, &discoveryv1.EndpointSlice{})
	require.NoError(t, gatewayv1.Install(scheme))

	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-gw",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}
	gatewayClass := &gwtypes.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-class",
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-svc",
		},
	}
	port := gwtypes.PortNumber(80)
	tcpRoute := &gwtypes.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.TCPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name: gwtypes.ObjectName("test-gw"),
				}},
			},
			Rules: []gwtypes.TCPRouteRule{{
				BackendRefs: []gwtypes.BackendRef{{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name: gwtypes.ObjectName("test-svc"),
						Port: &port,
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
		WithObjects(gatewayClass, gateway, svc, tcpRoute, epSlice).
		WithIndex(&gwtypes.Gateway{}, index.GatewayClassOnGatewayIndex, index.GatewayClassOnGateway).
		WithIndex(&gwtypes.TCPRoute{}, index.GatewayOnTCPRouteIndex, index.GatewaysOnRoute[gwtypes.TCPRoute]).
		WithIndex(&gwtypes.TCPRoute{}, index.BackendServicesOnTCPRouteIndex, index.BackendServicesOnTCPRoute).
		Build()

	tests := []struct {
		name    string
		mapFunc func(context.Context, client.Object) []reconcile.Request
		obj     client.Object
	}{
		{
			name:    "gateway maps to TCPRoute",
			mapFunc: MapRouteForGateway(cl, gwtypes.TCPRoute{}),
			obj:     gateway,
		},
		{
			name:    "gatewayclass maps to TCPRoute",
			mapFunc: MapRouteForGatewayClass(cl, gwtypes.TCPRoute{}),
			obj:     gatewayClass,
		},
		{
			name:    "service maps to TCPRoute",
			mapFunc: MapRouteForService(cl, gwtypes.TCPRoute{}),
			obj:     svc,
		},
		{
			name:    "endpointslice maps to TCPRoute",
			mapFunc: MapRouteForEndpointSlice(cl, gwtypes.TCPRoute{}),
			obj:     epSlice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := tt.mapFunc(context.Background(), tt.obj)
			require.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "test-ns", Name: "route-1"}}}, requests)
		})
	}
}

func TestMapTCPRouteForReferenceGrant(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, gatewayv1.Install(scheme))

	toNamespace := gwtypes.Namespace("to-ns")
	tcpRoute := &gwtypes.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "from-ns",
			Name:      "route-1",
		},
		Spec: gwtypes.TCPRouteSpec{
			Rules: []gwtypes.TCPRouteRule{{
				BackendRefs: []gwtypes.BackendRef{{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Namespace: &toNamespace,
						Name:      gwtypes.ObjectName("svc"),
					},
				}},
			}},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(tcpRoute).
		Build()
	referenceGrant := &gwtypes.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "to-ns",
			Name:      "grant",
		},
		Spec: gwtypes.ReferenceGrantSpec{
			From: []gwtypes.ReferenceGrantFrom{{
				Group:     gwtypes.GroupName,
				Kind:      "TCPRoute",
				Namespace: "from-ns",
			}},
		},
	}

	requests := MapTCPRouteForReferenceGrant(cl)(context.Background(), referenceGrant)

	require.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "from-ns", Name: "route-1"}}}, requests)
}

func TestMapRouteForKongResourceTCPRoute(t *testing.T) {
	obj := &configurationv1alpha1.KongUpstream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obj",
			Namespace: "test-ns",
			Annotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTCPRouteAnnotation: "ns1/route-1,ns2/route-2",
			},
		},
	}

	requests := MapRouteForKongResource[*configurationv1alpha1.KongUpstream](kindTCPRoute)(context.Background(), obj)

	require.ElementsMatch(t, []reconcile.Request{
		{NamespacedName: types.NamespacedName{Namespace: "ns1", Name: "route-1"}},
		{NamespacedName: types.NamespacedName{Namespace: "ns2", Name: "route-2"}},
	}, requests)
}
