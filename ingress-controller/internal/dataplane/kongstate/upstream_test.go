package kongstate

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"

	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
)

func TestUpstreamOverrideByKongUpstreamPolicy(t *testing.T) {
	testCases := []struct {
		name               string
		upstream           kong.Upstream
		kongUpstreamPolicy *configurationv1beta1.KongUpstreamPolicy
		expected           kong.Upstream
	}{
		{
			name: "algorithm, slots, healthchecks, hash_on, hash_fallback, hash_fallback_header",
			upstream: kong.Upstream{
				Algorithm: new("Algorithm"),
				Slots:     new(42),
				Healthchecks: &kong.Healthcheck{
					Active: &kong.ActiveHealthcheck{
						Concurrency: new(1),
					},
				},
				HashOn:             new("HashOn"),
				HashFallback:       new("HashFallback"),
				HashFallbackHeader: new("HashFallbackHeader"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					Algorithm: new("least-connections"),
					Slots:     new(10),
					Healthchecks: &configurationv1beta1.KongUpstreamHealthcheck{
						Active: &configurationv1beta1.KongUpstreamActiveHealthcheck{
							Concurrency: new(2),
						},
					},
					HashOn: &configurationv1beta1.KongUpstreamHash{
						Input: new(configurationv1beta1.HashInput("consumer")),
					},
					HashOnFallback: &configurationv1beta1.KongUpstreamHash{
						Header: new("foo"),
					},
				},
			},
			expected: kong.Upstream{
				Algorithm: new("least-connections"),
				Slots:     new(10),
				Healthchecks: &kong.Healthcheck{
					Active: &kong.ActiveHealthcheck{
						Concurrency: new(2),
					},
				},
				HashOn:             new("consumer"),
				HashFallback:       new("header"),
				HashFallbackHeader: new("foo"),
			},
		},
		{
			name: "hash_on_header, hash_fallback_query_arg",
			upstream: kong.Upstream{
				HashOn:               new("HashOn"),
				HashFallback:         new("HashFallback"),
				HashOnHeader:         new("HashOnHeader"),
				HashFallbackQueryArg: new("HashOnQueryArg"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						Header: new("foo"),
					},
					HashOnFallback: &configurationv1beta1.KongUpstreamHash{
						QueryArg: new("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:               new("header"),
				HashFallback:         new("query_arg"),
				HashOnHeader:         new("foo"),
				HashFallbackQueryArg: new("foo"),
			},
		},
		{
			name: "hash_on_cookie, hash_on_cookie_path, hash_fallback_uri_capture",
			upstream: kong.Upstream{
				HashOn:                 new("HashOn"),
				HashFallback:           new("HashFallback"),
				HashOnCookie:           new("HashOnCookie"),
				HashOnCookiePath:       new("HashOnCookiePath"),
				HashFallbackURICapture: new("HashFallbackURICapture"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						Cookie:     new("foo"),
						CookiePath: new("/"),
					},
					HashOnFallback: &configurationv1beta1.KongUpstreamHash{
						URICapture: new("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:                 new("cookie"),
				HashFallback:           new("uri_capture"),
				HashOnCookie:           new("foo"),
				HashOnCookiePath:       new("/"),
				HashFallbackURICapture: new("foo"),
			},
		},
		{
			name: "hash_on_uri_capture",
			upstream: kong.Upstream{
				HashOn:           new("HashOn"),
				HashOnURICapture: new("HashOnURICapture"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						URICapture: new("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:           new("uri_capture"),
				HashOnURICapture: new("foo"),
			},
		},
		{
			name: "hash_on_query_arg",
			upstream: kong.Upstream{
				HashOn:         new("HashOn"),
				HashOnQueryArg: new("HashOnQueryArg"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						QueryArg: new("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:         new("query_arg"),
				HashOnQueryArg: new("foo"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := Upstream{Upstream: tc.upstream}
			upstream.overrideByKongUpstreamPolicy(tc.kongUpstreamPolicy)
			require.Equal(t, tc.expected, upstream.Upstream)
		})
	}

	require.NotPanics(t, func() {
		var nilUpstream *Upstream
		nilUpstream.overrideByKongUpstreamPolicy(nil)
	})
}
