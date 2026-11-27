package translator

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestGenerateKongRoutesFromRouteRule_TCP(t *testing.T) {
	testCases := []struct {
		name      string
		route     *gwtypes.TCPRoute
		gwPorts   []gwtypes.PortNumber
		routeRule gwtypes.TCPRouteRule
		expected  []kongstate.Route
	}{
		{
			name: "TCPRoute gets translated correctly to kong.Route",
			route: &gwtypes.TCPRoute{
				Name:      "mytcproute-name",
				Namespace: "mynamespace",
			},
			gwPorts: []gwtypes.PortNumber{8080},
			routeRule: gwtypes.TCPRouteRule{
				BackendRefs: []gwtypes.BackendRef{
					{
						Port: new(gwtypes.PortNumber(1234)),
					},
				},
			},
			expected: []kongstate.Route{
				{
					Ingress: util.K8sObjectInfo{
						Name:      "mytcproute-name",
						Namespace: "mynamespace",
					},
					Name: new("tcproute.mynamespace.mytcproute-name.0.0"),
					Destinations: []*kong.CIDRPort{
						{
							Port: new(8080),
						},
					},
					Protocols: []*string{
						new("tcp"),
					},
					Tags: []*string{
						new("k8s-name:mytcproute-name"),
						new("k8s-namespace:mynamespace"),
					},
				},
			},
		},
		{
			name: "TCPRoute with multiple backends and different Gateway port get translated correctly to kong.Route",
			route: &gwtypes.TCPRoute{
				Name:      "mytcproute-name",
				Namespace: "mynamespace",
			},
			routeRule: gwtypes.TCPRouteRule{
				BackendRefs: []gwtypes.BackendRef{
					{
						Port: new(gwtypes.PortNumber(1234)),
					},
					{
						Port: new(gwtypes.PortNumber(5678)),
					},
				},
			},
			gwPorts: []gwtypes.PortNumber{8080, 8888},
			expected: []kongstate.Route{
				{
					Ingress: util.K8sObjectInfo{
						Name:      "mytcproute-name",
						Namespace: "mynamespace",
					},
					Name: new("tcproute.mynamespace.mytcproute-name.0.0"),
					Destinations: []*kong.CIDRPort{
						{
							Port: new(8080),
						},
						{
							Port: new(8888),
						},
					},
					Protocols: []*string{
						new("tcp"),
					},
					Tags: []*string{
						new("k8s-name:mytcproute-name"),
						new("k8s-namespace:mynamespace"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kongRoutes, err := generateKongRoutesFromRouteRule(tc.route, tc.gwPorts, 0, tc.routeRule)
			require.NoError(t, err)
			require.NotNil(t, kongRoutes)
			require.Equal(t, tc.expected, kongRoutes)
		})
	}
}

func TestGenerateKongRoutesFromRouteRule_UDP(t *testing.T) {
	testCases := []struct {
		name      string
		route     *gwtypes.UDPRoute
		gwPorts   []gwtypes.PortNumber
		routeRule gwtypes.UDPRouteRule
		expected  []kongstate.Route
	}{
		{
			name: "UDPRoute gets translated correctly to kong.Route",
			route: &gwtypes.UDPRoute{
				Name:      "myudproute-name",
				Namespace: "mynamespace",
			},
			routeRule: gwtypes.UDPRouteRule{
				BackendRefs: []gwtypes.BackendRef{
					{
						Port: new(gwtypes.PortNumber(1234)),
					},
				},
			},
			gwPorts: []gwtypes.PortNumber{8080},
			expected: []kongstate.Route{
				{
					Ingress: util.K8sObjectInfo{
						Name:      "myudproute-name",
						Namespace: "mynamespace",
					},
					Name: new("udproute.mynamespace.myudproute-name.0.0"),
					Destinations: []*kong.CIDRPort{
						{
							Port: new(8080),
						},
					},
					Protocols: []*string{
						new("udp"),
					},
					Tags: []*string{
						new("k8s-name:myudproute-name"),
						new("k8s-namespace:mynamespace"),
					},
				},
			},
		},
		{
			name: "UDPRoute with multiple backends and multiple Gateway ports gets translated correctly to kong.Route",
			route: &gwtypes.UDPRoute{
				Name:      "myudproute-name",
				Namespace: "mynamespace",
			},
			routeRule: gwtypes.UDPRouteRule{
				BackendRefs: []gwtypes.BackendRef{
					{
						Port: new(gwtypes.PortNumber(1234)),
					},
					{
						Port: new(gwtypes.PortNumber(5678)),
					},
				},
			},
			gwPorts: []gwtypes.PortNumber{8080, 8888},
			expected: []kongstate.Route{
				{
					Ingress: util.K8sObjectInfo{
						Name:      "myudproute-name",
						Namespace: "mynamespace",
					},
					Name: new("udproute.mynamespace.myudproute-name.0.0"),
					Destinations: []*kong.CIDRPort{
						{
							Port: new(8080),
						},
						{
							Port: new(8888),
						},
					},
					Protocols: []*string{
						new("udp"),
					},
					Tags: []*string{
						new("k8s-name:myudproute-name"),
						new("k8s-namespace:mynamespace"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kongRoutes, err := generateKongRoutesFromRouteRule(tc.route, tc.gwPorts, 0, tc.routeRule)
			require.NoError(t, err)
			require.NotNil(t, kongRoutes)
			require.Equal(t, tc.expected, kongRoutes)
		})
	}
}

func TestGenerateKongRoutesFromRouteRule_TLS(t *testing.T) {
	testcases := []struct {
		name      string
		route     *gwtypes.TLSRoute
		routeRule gwtypes.TLSRouteRule
		expected  []kongstate.Route
	}{
		{
			name: "TLSRoute gets translated correctly to kong.Route",
			route: &gwtypes.TLSRoute{
				Name:      "mytlsroute-name",
				Namespace: "mynamespace",
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"hostname.com",
						"hostname2.com",
					},
				},
			},
			routeRule: gwtypes.TLSRouteRule{},
			expected: []kongstate.Route{
				{
					Ingress: util.K8sObjectInfo{
						Name:      "mytlsroute-name",
						Namespace: "mynamespace",
					},
					Name: new("tlsroute.mynamespace.mytlsroute-name.0.0"),
					SNIs: []*string{
						new("hostname.com"),
						new("hostname2.com"),
					},
					Protocols: []*string{
						new("tls"),
					},
					Tags: []*string{
						new("k8s-name:mytlsroute-name"),
						new("k8s-namespace:mynamespace"),
					},
				},
			},
		},
		{
			name: "TLSRoute without hostnames gets translated correctly to kong.Route without SNIs",
			route: &gwtypes.TLSRoute{
				Name:      "mytlsroute-name",
				Namespace: "mynamespace",
				Spec:      gwtypes.TLSRouteSpec{},
			},
			routeRule: gwtypes.TLSRouteRule{},
			expected: []kongstate.Route{
				{
					Ingress: util.K8sObjectInfo{
						Name:      "mytlsroute-name",
						Namespace: "mynamespace",
					},
					Name: new("tlsroute.mynamespace.mytlsroute-name.0.0"),
					SNIs: []*string{},
					Protocols: []*string{
						new("tls"),
					},
					Tags: []*string{
						new("k8s-name:mytlsroute-name"),
						new("k8s-namespace:mynamespace"),
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// TLSRoute matches based on hostname with Gateway listener thus passing gwPorts is pointless.
			kongRoutes, err := generateKongRoutesFromRouteRule(tc.route, nil, 0, tc.routeRule)
			require.NoError(t, err)
			require.NotNil(t, kongRoutes)
			require.Equal(t, tc.expected, kongRoutes)
		})
	}
}
