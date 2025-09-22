package kongstate

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
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
				Algorithm: kong.String("Algorithm"),
				Slots:     kong.Int(42),
				Healthchecks: &kong.Healthcheck{
					Active: &kong.ActiveHealthcheck{
						Concurrency: kong.Int(1),
					},
				},
				HashOn:             kong.String("HashOn"),
				HashFallback:       kong.String("HashFallback"),
				HashFallbackHeader: kong.String("HashFallbackHeader"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					Algorithm: lo.ToPtr("least-connections"),
					Slots:     lo.ToPtr(10),
					Healthchecks: &configurationv1beta1.KongUpstreamHealthcheck{
						Active: &configurationv1beta1.KongUpstreamActiveHealthcheck{
							Concurrency: lo.ToPtr(2),
						},
					},
					HashOn: &configurationv1beta1.KongUpstreamHash{
						Input: lo.ToPtr(configurationv1beta1.HashInput("consumer")),
					},
					HashOnFallback: &configurationv1beta1.KongUpstreamHash{
						Header: lo.ToPtr("foo"),
					},
				},
			},
			expected: kong.Upstream{
				Algorithm: kong.String("least-connections"),
				Slots:     kong.Int(10),
				Healthchecks: &kong.Healthcheck{
					Active: &kong.ActiveHealthcheck{
						Concurrency: kong.Int(2),
					},
				},
				HashOn:             kong.String("consumer"),
				HashFallback:       kong.String("header"),
				HashFallbackHeader: kong.String("foo"),
			},
		},
		{
			name: "hash_on_header, hash_fallback_query_arg",
			upstream: kong.Upstream{
				HashOn:               kong.String("HashOn"),
				HashFallback:         kong.String("HashFallback"),
				HashOnHeader:         kong.String("HashOnHeader"),
				HashFallbackQueryArg: kong.String("HashOnQueryArg"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						Header: lo.ToPtr("foo"),
					},
					HashOnFallback: &configurationv1beta1.KongUpstreamHash{
						QueryArg: lo.ToPtr("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:               kong.String("header"),
				HashFallback:         kong.String("query_arg"),
				HashOnHeader:         kong.String("foo"),
				HashFallbackQueryArg: kong.String("foo"),
			},
		},
		{
			name: "hash_on_cookie, hash_on_cookie_path, hash_fallback_uri_capture",
			upstream: kong.Upstream{
				HashOn:                 kong.String("HashOn"),
				HashFallback:           kong.String("HashFallback"),
				HashOnCookie:           kong.String("HashOnCookie"),
				HashOnCookiePath:       kong.String("HashOnCookiePath"),
				HashFallbackURICapture: kong.String("HashFallbackURICapture"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						Cookie:     lo.ToPtr("foo"),
						CookiePath: lo.ToPtr("/"),
					},
					HashOnFallback: &configurationv1beta1.KongUpstreamHash{
						URICapture: lo.ToPtr("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:                 kong.String("cookie"),
				HashFallback:           kong.String("uri_capture"),
				HashOnCookie:           kong.String("foo"),
				HashOnCookiePath:       kong.String("/"),
				HashFallbackURICapture: kong.String("foo"),
			},
		},
		{
			name: "hash_on_uri_capture",
			upstream: kong.Upstream{
				HashOn:           kong.String("HashOn"),
				HashOnURICapture: kong.String("HashOnURICapture"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						URICapture: lo.ToPtr("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:           kong.String("uri_capture"),
				HashOnURICapture: kong.String("foo"),
			},
		},
		{
			name: "hash_on_query_arg",
			upstream: kong.Upstream{
				HashOn:         kong.String("HashOn"),
				HashOnQueryArg: kong.String("HashOnQueryArg"),
			},
			kongUpstreamPolicy: &configurationv1beta1.KongUpstreamPolicy{
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					HashOn: &configurationv1beta1.KongUpstreamHash{
						QueryArg: lo.ToPtr("foo"),
					},
				},
			},
			expected: kong.Upstream{
				HashOn:         kong.String("query_arg"),
				HashOnQueryArg: kong.String("foo"),
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
