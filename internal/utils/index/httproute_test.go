package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestBackendRefOnHTTPRoute(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "no backendRefs",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{},
					},
				},
			},
			want: nil,
		},
		{
			name: "single backendRef with default group/kind in single rule",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
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
			},
			want: []string{"ns1/svc1"},
		},
		{
			name: "backendRef in different namespaces",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Group: ptrGroup("core"),
											Kind:  ptrKind("Service"),
											Name:  gatewayv1.ObjectName("svc1"),
											Port:  ptrPort(80),
										},
									},
								},
							},
						},
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
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
			},
			want: []string{"ns1/svc1", "ns2/svc2"},
		},
		{
			name: "unmatched group/kind",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
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
			},
			want: nil,
		},
		{
			name: "empty port",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: gatewayv1.ObjectName("svc1"),
										},
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
			got := backendServicesOnHTTPRoute(tc.obj)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestKongPluginsOnHTTPRoute(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "no filters",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{},
					},
				},
			},
			want: nil,
		},
		{
			name: "single filter with extenstionRef type",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							Filters: []gwtypes.HTTPRouteFilter{
								{
									Type: gwtypes.HTTPRouteFilterExtensionRef,
									ExtensionRef: &gwtypes.LocalObjectReference{
										Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
										Kind:  gatewayv1.Kind("KongPlugin"),
										Name:  gatewayv1.ObjectName("rate-limiting-1"),
									},
								},
							},
						},
					},
				},
			},
			want: []string{"ns1/rate-limiting-1"},
		},
		{
			name: "extensionRef with unmatched group/kind",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							Filters: []gwtypes.HTTPRouteFilter{
								{
									Type: gwtypes.HTTPRouteFilterExtensionRef,
									ExtensionRef: &gwtypes.LocalObjectReference{
										Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
										Kind:  gatewayv1.Kind("KongConsumer"),
										Name:  gatewayv1.ObjectName("consumer-1"),
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
			name: "multiple exensionRefs with duplicate names",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							Filters: []gwtypes.HTTPRouteFilter{
								{
									Type: gwtypes.HTTPRouteFilterExtensionRef,
									ExtensionRef: &gwtypes.LocalObjectReference{
										Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
										Kind:  gatewayv1.Kind("KongPlugin"),
										Name:  gatewayv1.ObjectName("rate-limiting-1"),
									},
								},
								{
									Type: gwtypes.HTTPRouteFilterExtensionRef,
									ExtensionRef: &gwtypes.LocalObjectReference{
										Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
										Kind:  gatewayv1.Kind("KongPlugin"),
										Name:  gatewayv1.ObjectName("key-auth-1"),
									},
								},
							},
						},
						{
							Filters: []gwtypes.HTTPRouteFilter{
								{
									Type: gwtypes.HTTPRouteFilterExtensionRef,
									ExtensionRef: &gwtypes.LocalObjectReference{
										Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
										Kind:  gatewayv1.Kind("KongPlugin"),
										Name:  gatewayv1.ObjectName("rate-limiting-1"),
									},
								},
								{
									Type: gwtypes.HTTPRouteFilterExtensionRef,
									ExtensionRef: &gwtypes.LocalObjectReference{
										Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
										Kind:  gatewayv1.Kind("KongPlugin"),
										Name:  gatewayv1.ObjectName("key-auth-2"),
									},
								},
							},
						},
					},
				},
			},
			want: []string{"ns1/key-auth-1", "ns1/key-auth-2", "ns1/rate-limiting-1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := KongPluginsOnHTTPRoute(tc.obj)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}
