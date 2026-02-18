package subtranslator

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
)

func TestApplyExpressionToL4KongRoute(t *testing.T) {
	testCases := []struct {
		name    string
		route   kong.Route
		subExpr string
	}{
		{
			name:    "destination port",
			subExpr: "net.dst.port == 1234",
			route: kong.Route{
				Destinations: []*kong.CIDRPort{
					{
						Port: new(1234),
					},
				},
				Protocols: []*string{
					new("tcp"),
				},
			},
		},
		{
			name:    "multiple destination ports",
			subExpr: "(net.dst.port == 1234) || (net.dst.port == 5678)",
			route: kong.Route{
				Destinations: []*kong.CIDRPort{
					{
						Port: new(1234),
					},
					{
						Port: new(5678),
					},
				},
				Protocols: []*string{
					new("tcp"),
				},
			},
		},
		{
			name:    "SNI host",
			subExpr: "tls.sni == \"example.com\"",
			route: kong.Route{
				SNIs: []*string{
					new("example.com"),
				},
				Protocols: []*string{
					new("tcp"),
				},
			},
		},
		{
			name:    "multiple SNI hosts",
			subExpr: "(tls.sni == \"example.com\") || (tls.sni == \"example.net\")",
			route: kong.Route{
				SNIs: []*string{
					new("example.com"),
					new("example.net"),
				},
				Protocols: []*string{
					new("tcp"),
				},
			},
		},
		{
			name:    "SNI host and multiple destination ports",
			subExpr: "(tls.sni == \"example.com\") && ((net.dst.port == 1234) || (net.dst.port == 5678))",
			route: kong.Route{
				Destinations: []*kong.CIDRPort{
					{
						Port: new(1234),
					},
					{
						Port: new(5678),
					},
				},
				SNIs: []*string{
					new("example.com"),
				},
				Protocols: []*string{
					new("tcp"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := kongstate.Route{Route: tc.route}
			ApplyExpressionToL4KongRoute(&wrapped)
			require.Contains(t, *wrapped.Expression, tc.subExpr)
		})
	}
}
