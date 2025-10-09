package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestGatewaysOnHTTPRoute(t *testing.T) {
	tests := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "single parentref, default ns",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name: "gw1",
							},
						},
					},
				},
			},
			want: []string{"ns1/gw1"},
		},
		{
			name: "parentref with explicit ns",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:      "gw2",
								Namespace: ptrNamespace("ns2"),
							},
						},
					},
				},
			},
			want: []string{"ns2/gw2"},
		},
		{
			name: "parentref with non-Gateway kind",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name: "gw3",
								Kind: ptrKind("OtherKind"),
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "parentref with non-gateway group",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:  "gw4",
								Group: ptrGroup("other.group"),
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "multiple parentrefs, dedup",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "gw1"},
							{Name: "gw1"},
							{Name: "gw2", Namespace: ptrNamespace("ns2")},
						},
					},
				},
			},
			want: []string{"ns1/gw1", "ns2/gw2"},
		},
		{
			name: "wrong type",
			obj:  &gwtypes.Gateway{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GatewaysOnHTTPRoute(tt.obj)
			require.ElementsMatch(t, tt.want, got)
		})
	}
}

func ptrNamespace(s string) *gatewayv1.Namespace {
	ns := gatewayv1.Namespace(s)
	return &ns
}
func ptrKind(s string) *gatewayv1.Kind {
	k := gatewayv1.Kind(s)
	return &k
}
func ptrGroup(s string) *gatewayv1.Group {
	g := gatewayv1.Group(s)
	return &g
}
