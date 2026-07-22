package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestBackendRefOnTCPRoute(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "no backendRefs",
			obj: &gwtypes.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.TCPRouteSpec{
					Rules: []gwtypes.TCPRouteRule{
						{},
					},
				},
			},
			want: nil,
		},
		{
			name: "single backendRef with default group/kind in single rule",
			obj: &gwtypes.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.TCPRouteSpec{
					Rules: []gwtypes.TCPRouteRule{
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Name: gatewayv1.ObjectName("svc1"),
										Port: ptrPort(80),
									},
								},
							},
						},
					},
				},
			},
			want: []string{"ns1/svc1"},
		},
		{
			name: "backendRef in different namespaces",
			obj: &gwtypes.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.TCPRouteSpec{
					Rules: []gwtypes.TCPRouteRule{
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Group: ptrGroup("core"),
										Kind:  ptrKind("Service"),
										Name:  gatewayv1.ObjectName("svc1"),
										Port:  ptrPort(80),
									},
								},
							},
						},
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Namespace: ptrNamespace("ns2"),
										Name:      gatewayv1.ObjectName("svc2"),
										Port:      ptrPort(80),
									},
								},
							},
						},
					},
				},
			},
			want: []string{"ns1/svc1", "ns2/svc2"},
		},
		{
			name: "unmatched group/kind",
			obj: &gwtypes.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.TCPRouteSpec{
					Rules: []gwtypes.TCPRouteRule{
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Group: ptrGroup("configuration.konghq.com"),
										Kind:  ptrKind("KongService"),
										Name:  gatewayv1.ObjectName("svc1"),
										Port:  ptrPort(8080),
									},
								},
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "empty port",
			obj: &gwtypes.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.TCPRouteSpec{
					Rules: []gwtypes.TCPRouteRule{
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Name: gatewayv1.ObjectName("svc1"),
									},
								},
							},
						},
					},
				},
			},
			want: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := BackendServicesOnTCPRoute(tc.obj)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}
