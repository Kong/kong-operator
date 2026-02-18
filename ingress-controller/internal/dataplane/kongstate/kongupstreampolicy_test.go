package kongstate_test

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
)

func TestGetKongUpstreamPolicyForServices(t *testing.T) {
	testCases := []struct {
		name          string
		servicesGroup []*corev1.Service
		policies      []*configurationv1beta1.KongUpstreamPolicy
		expectPolicy  bool
		expectError   string
	}{
		{
			name:         "no services in group gives no policy",
			expectPolicy: false,
		},
		{
			name: "no KongUpstreamPolicy in store while services are configured with one gives error",
			servicesGroup: []*corev1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
					Annotations: map[string]string{
						configurationv1beta1.KongUpstreamPolicyAnnotationKey: "upstream-policy",
					},
				},
			}},
			expectError: "failed fetching KongUpstreamPolicy",
		},
		{
			name: "all services configured with no KongUpstreamPolicy gives no policy and no error",
			servicesGroup: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: "default",
					},
				},
			},
			expectPolicy: false,
		},
		{
			name: "services in group with different KongUpstreamPolicy configurations gives error",
			servicesGroup: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "upstream-policy",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: "default",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "other-upstream-policy",
						},
					},
				},
			},
			expectError: "inconsistent KongUpstreamPolicy configuration for services",
		},
		{
			name: "one service with and one without KongUpstreamPolicy configuration gives error",
			servicesGroup: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "upstream-policy",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: "default",
					},
				},
			},
			expectError: "inconsistent KongUpstreamPolicy configuration for services",
		},
		{
			name: "all the services configured with the same KongUpstreamPolicy gives the policy",
			servicesGroup: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "upstream-policy",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: "default",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "upstream-policy",
						},
					},
				},
			},
			policies: []*configurationv1beta1.KongUpstreamPolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "upstream-policy",
						Namespace: "default",
					},
				},
			},
			expectPolicy: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := store.NewFakeStore(store.FakeObjects{
				Services:             tc.servicesGroup,
				KongUpstreamPolicies: tc.policies,
			})
			require.NoError(t, err)

			policy, err := kongstate.GetKongUpstreamPolicyForServices(s, tc.servicesGroup)
			if tc.expectError != "" {
				require.ErrorContains(t, err, tc.expectError)
				return
			}
			if tc.expectPolicy {
				require.NotNil(t, policy)
			} else {
				require.Nil(t, policy)
			}
		})
	}
}

func TestTranslateKongUpstreamPolicy(t *testing.T) {
	testCases := []struct {
		name             string
		policySpec       configurationv1beta1.KongUpstreamPolicySpec
		expectedUpstream *kong.Upstream
	}{
		{
			name: "KongUpstreamPolicySpec with no hash-on or hash-fallback",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Algorithm: new("least-connections"),
				Slots:     new(10),
			},
			expectedUpstream: &kong.Upstream{
				Algorithm: new("least-connections"),
				Slots:     new(10),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on header",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Header: new("foo"),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Header: new("bar"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:             new("header"),
				HashOnHeader:       new("foo"),
				HashFallback:       new("header"),
				HashFallbackHeader: new("bar"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on header and hash-on fallback cookie",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Header: new("foo"),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("cookie-name"),
					CookiePath: new("/cookie-path"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:           new("header"),
				HashOnHeader:     new("foo"),
				HashFallback:     new("cookie"),
				HashOnCookie:     new("cookie-name"),
				HashOnCookiePath: new("/cookie-path"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on query-arg and hash-on fallback cookie",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					QueryArg: new("foo"),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("cookie-name"),
					CookiePath: new("/cookie-path"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:           new("query_arg"),
				HashOnQueryArg:   new("foo"),
				HashFallback:     new("cookie"),
				HashOnCookie:     new("cookie-name"),
				HashOnCookiePath: new("/cookie-path"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on uri-capture and hash-on fallback cookie",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					URICapture: new("foo"),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("cookie-name"),
					CookiePath: new("/cookie-path"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:           new("uri_capture"),
				HashOnURICapture: new("foo"),
				HashFallback:     new("cookie"),
				HashOnCookie:     new("cookie-name"),
				HashOnCookiePath: new("/cookie-path"),
			},
		},
		{
			// This will be blocked by CRD validation rules because according to
			// https://developer.konghq.com/gateway/entities/upstream/#consistent-hashing
			// if the primary hash_on is set to cookie, the hash_fallback is invalid
			// and cannot be used.
			name: "KongUpstreamPolicySpec with hash-on cookie and hash-on fallback cookie is incorrect and should not happen",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("cookie-name"),
					CookiePath: new("/cookie-path"),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("cookie-name-2"),
					CookiePath: new("/cookie-path-2"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:           new("cookie"),
				HashOnCookie:     new("cookie-name"),
				HashOnCookiePath: new("/cookie-path"),
				HashFallback:     new("cookie"),
			},
		},
		{
			// This will be blocked by CRD validation rules because according to
			// https://developer.konghq.com/gateway/entities/upstream/#consistent-hashing
			// if the primary hash_on is set to cookie, the hash_fallback is invalid
			// and cannot be used.
			name: "KongUpstreamPolicySpec with hash-on cookie and hash-on fallback header is incorrect and should not happen",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("cookie-name"),
					CookiePath: new("/cookie-path"),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Header: new("header-name"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:             new("cookie"),
				HashOnCookie:       new("cookie-name"),
				HashOnCookiePath:   new("/cookie-path"),
				HashFallback:       new("header"),
				HashFallbackHeader: new("header-name"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on cookie",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("foo"),
					CookiePath: new("/"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:           new("cookie"),
				HashOnCookie:     new("foo"),
				HashOnCookiePath: new("/"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on query-arg",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					QueryArg: new("foo"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:         new("query_arg"),
				HashOnQueryArg: new("foo"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on uri-capture",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					URICapture: new("foo"),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:           new("uri_capture"),
				HashOnURICapture: new("foo"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with predefined hash input",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Input: new(configurationv1beta1.HashInput("consumer")),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Input: new(configurationv1beta1.HashInput("ip")),
				},
			},
			expectedUpstream: &kong.Upstream{
				HashOn:       new("consumer"),
				HashFallback: new("ip"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with healthchecks",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Healthchecks: &configurationv1beta1.KongUpstreamHealthcheck{
					Active: &configurationv1beta1.KongUpstreamActiveHealthcheck{
						Type:        new("http"),
						Concurrency: new(10),
						Healthy: &configurationv1beta1.KongUpstreamHealthcheckHealthy{
							HTTPStatuses: []configurationv1beta1.HTTPStatus{200},
							Interval:     new(20),
							Successes:    new(30),
						},
						Unhealthy: &configurationv1beta1.KongUpstreamHealthcheckUnhealthy{
							HTTPFailures: new(40),
							HTTPStatuses: []configurationv1beta1.HTTPStatus{500},
							Timeouts:     new(60),
							Interval:     new(70),
						},
						HTTPPath:               new("/foo"),
						HTTPSSNI:               new("foo.com"),
						HTTPSVerifyCertificate: new(true),
						Timeout:                new(80),
						Headers:                map[string][]string{"foo": {"bar"}},
					},
					Passive: &configurationv1beta1.KongUpstreamPassiveHealthcheck{
						Type: new("tcp"),
						Healthy: &configurationv1beta1.KongUpstreamHealthcheckHealthy{
							Successes: new(100),
						},
						Unhealthy: &configurationv1beta1.KongUpstreamHealthcheckUnhealthy{
							TCPFailures: new(110),
							Timeouts:    new(120),
						},
					},
					Threshold: new(10),
				},
			},
			expectedUpstream: &kong.Upstream{
				Healthchecks: &kong.Healthcheck{
					Active: &kong.ActiveHealthcheck{
						Type:        new("http"),
						Concurrency: new(10),
						Healthy: &kong.Healthy{
							HTTPStatuses: []int{200},
							Interval:     new(20),
							Successes:    new(30),
						},
						Unhealthy: &kong.Unhealthy{
							HTTPFailures: new(40),
							HTTPStatuses: []int{500},
							Timeouts:     new(60),
							Interval:     new(70),
						},
						HTTPPath:               new("/foo"),
						HTTPSSni:               new("foo.com"),
						HTTPSVerifyCertificate: new(true),
						Headers:                map[string][]string{"foo": {"bar"}},
						Timeout:                new(80),
					},
					Passive: &kong.PassiveHealthcheck{
						Type: new("tcp"),
						Healthy: &kong.Healthy{
							Successes: new(100),
						},
						Unhealthy: &kong.Unhealthy{
							TCPFailures: new(110),
							Timeouts:    new(120),
						},
					},
					Threshold: new(float64(10.0)),
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualUpstream := kongstate.TranslateKongUpstreamPolicy(tc.policySpec)
			require.Equal(t, tc.expectedUpstream, actualUpstream)
		})
	}
}
