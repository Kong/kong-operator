package kongintegration

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/adminapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/sendconfig"
	managercfg "github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/v2/ingress-controller/test/kongintegration/containers"
)

// TestKongUpstreamPolicyTranslation ensures that the Upstream Policy CRD is translated to the Kong Upstream
// object in a way that when it's sent to Kong, all the fields are correctly propagated.
func TestKongUpstreamPolicyTranslation(t *testing.T) {
	t.Parallel()

	const (
		timeout = time.Second * 1
		period  = time.Millisecond * 100
	)

	ctx := t.Context()

	kongC := containers.NewKong(ctx, t)
	kongClient, err := adminapi.NewKongAPIClient(kongC.AdminURL(ctx, t), managercfg.AdminAPIClientConfig{}, "")
	require.NoError(t, err)
	updateStrategy := sendconfig.NewUpdateStrategyInMemory(
		kongClient,
		sendconfig.DefaultContentToDBLessConfigConverter{},
		logr.Discard(),
	)

	testCases := []struct {
		name             string
		policySpec       configurationv1beta1.KongUpstreamPolicySpec
		expectedUpstream *kong.Upstream
	}{
		{
			name: "KongUpstreamPolicySpec with no hash-on or hash-fallback",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Algorithm: new("least-connections"),
				Slots:     new(20),
			},
			expectedUpstream: &kong.Upstream{
				Algorithm: new("least-connections"),
				Slots:     new(20),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on header",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Algorithm: new("consistent-hashing"),
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Header: new("foo"),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Header: new("bar"),
				},
			},
			expectedUpstream: &kong.Upstream{
				Algorithm:          new("consistent-hashing"),
				HashOn:             new("header"),
				HashOnHeader:       new("foo"),
				HashFallback:       new("header"),
				HashFallbackHeader: new("bar"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on cookie",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Algorithm: new("consistent-hashing"),
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Cookie:     new("foo"),
					CookiePath: new("/"),
				},
			},
			expectedUpstream: &kong.Upstream{
				Algorithm:        new("consistent-hashing"),
				HashOn:           new("cookie"),
				HashOnCookie:     new("foo"),
				HashOnCookiePath: new("/"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on query-arg",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Algorithm: new("consistent-hashing"),
				HashOn: &configurationv1beta1.KongUpstreamHash{
					QueryArg: new("foo"),
				},
			},
			expectedUpstream: &kong.Upstream{
				Algorithm:      new("consistent-hashing"),
				HashOn:         new("query_arg"),
				HashOnQueryArg: new("foo"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with predefined hash input",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Algorithm: new("consistent-hashing"),
				HashOn: &configurationv1beta1.KongUpstreamHash{
					Input: new(configurationv1beta1.HashInput("consumer")),
				},
				HashOnFallback: &configurationv1beta1.KongUpstreamHash{
					Input: new(configurationv1beta1.HashInput("ip")),
				},
			},
			expectedUpstream: &kong.Upstream{
				Algorithm:    new("consistent-hashing"),
				HashOn:       new("consumer"),
				HashFallback: new("ip"),
			},
		},
		{
			name: "KongUpstreamPolicySpec with hash-on uri-capture",
			policySpec: configurationv1beta1.KongUpstreamPolicySpec{
				Algorithm: new("consistent-hashing"),
				HashOn: &configurationv1beta1.KongUpstreamHash{
					URICapture: new("foo"),
				},
			},
			expectedUpstream: &kong.Upstream{
				Algorithm:        new("consistent-hashing"),
				HashOn:           new("uri_capture"),
				HashOnURICapture: new("foo"),
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
							TCPFailures:  new(5),
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
							HTTPStatuses: []configurationv1beta1.HTTPStatus{200},
							Successes:    new(100),
						},
						Unhealthy: &configurationv1beta1.KongUpstreamHealthcheckUnhealthy{
							HTTPStatuses: []configurationv1beta1.HTTPStatus{500},
							TCPFailures:  new(110),
							Timeouts:     new(120),
						},
					},
					Threshold: new(15),
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
							TCPFailures:  new(5),
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
							HTTPStatuses: []int{200},
							Successes:    new(100),
						},
						Unhealthy: &kong.Unhealthy{
							HTTPFailures: new(0),
							HTTPStatuses: []int{500},
							TCPFailures:  new(110),
							Timeouts:     new(120),
						},
					},
					Threshold: new(15.0),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			translatedUpstream := kongstate.TranslateKongUpstreamPolicy(tc.policySpec)
			const upstreamName = "test-upstream"
			translatedUpstream.Name = new(upstreamName)
			tc.expectedUpstream.Name = new(upstreamName)

			content := sendconfig.ContentWithHash{
				Content: &file.Content{
					FormatVersion: "3.0",
					Upstreams: []file.FUpstream{
						{
							Upstream: *translatedUpstream,
						},
					},
				},
			}

			// Update Kong with the Upstream.
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				_, err := updateStrategy.Update(ctx, content)
				assert.NoError(c, err)
			}, timeout, period)

			// Wait for the Upstream to be created in Kong and assert it matches the expected Upstream.
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				upstreamInKong, err := kongClient.Upstreams.Get(ctx, new(upstreamName))
				if !assert.NoError(c, err, "getting upstream from Kong") {
					return
				}

				// We ignore the values that are generated by Kong and are not known in advance.
				ignoreKongGeneratedValues := cmp.Transformer(
					"IgnoreKongGeneratedValues",
					func(upstream *kong.Upstream) *kong.Upstream {
						return dropIDsAndTimestamps(dropKongDefaults(upstream))
					},
				)
				diff := cmp.Diff(upstreamInKong, tc.expectedUpstream, ignoreKongGeneratedValues)
				assert.Empty(c, diff, "upstream in Kong does not match expected upstream")
			}, timeout, period)
		})
	}
}

// dropIDsAndTimestamps drops the ID and CreatedAt fields from the Upstream. These fields are generated by Kong and
// are not known in advance, so we want to ignore them when comparing the Upstream in Kong with the expected Upstream.
func dropIDsAndTimestamps(upstream *kong.Upstream) *kong.Upstream {
	upstream = upstream.DeepCopy()
	upstream.ID = nil
	upstream.CreatedAt = nil
	return upstream
}

// dropKongDefaults drops the default values that Kong sets for some fields. We offload the responsibility of setting
// these default values to Kong, so we want to ignore them when comparing the Upstream in Kong with the expected one.
func dropKongDefaults(upstream *kong.Upstream) *kong.Upstream {
	upstream = upstream.DeepCopy()

	defaultHealthcheck := &kong.Healthcheck{
		Active: &kong.ActiveHealthcheck{
			Concurrency: new(10),
			Healthy: &kong.Healthy{
				HTTPStatuses: []int{200, 302},
				Successes:    new(0),
				Interval:     new(0),
			},
			HTTPPath:               new("/"),
			HTTPSVerifyCertificate: new(true),
			Type:                   new("http"),
			Timeout:                new(1),
			Unhealthy: &kong.Unhealthy{
				HTTPFailures: new(0),
				HTTPStatuses: []int{429, 404, 500, 501, 502, 503, 504, 505},
				TCPFailures:  new(0),
				Timeouts:     new(0),
				Interval:     new(0),
			},
		},
		Passive: &kong.PassiveHealthcheck{
			Healthy: &kong.Healthy{
				HTTPStatuses: []int{200, 201, 202, 203, 204, 205, 206, 207, 208, 226, 300, 301, 302, 303, 304, 305, 306, 307, 308},
				Successes:    new(0),
			},
			Type: new("http"),
			Unhealthy: &kong.Unhealthy{
				HTTPFailures: new(0),
				HTTPStatuses: []int{429, 500, 503},
				TCPFailures:  new(0),
				Timeouts:     new(0),
			},
		},
		Threshold: new(0.),
	}

	if diff := cmp.Diff(upstream.Healthchecks, defaultHealthcheck); diff == "" {
		upstream.Healthchecks = nil
	}
	if upstream.HashOn != nil && *upstream.HashOn == "none" {
		upstream.HashOn = nil
	}
	if upstream.HashFallback != nil && *upstream.HashFallback == "none" {
		upstream.HashFallback = nil
	}
	if upstream.HashOnCookiePath != nil && *upstream.HashOnCookiePath == "/" {
		upstream.HashOnCookiePath = nil
	}
	if upstream.StickySessionsCookiePath != nil && *upstream.StickySessionsCookiePath == "/" {
		upstream.StickySessionsCookiePath = nil
	}
	if upstream.UseSrvName != nil && *upstream.UseSrvName == false {
		upstream.UseSrvName = nil
	}
	if upstream.Slots != nil && *upstream.Slots == 10000 {
		upstream.Slots = nil
	}
	if upstream.Algorithm != nil && *upstream.Algorithm == "round-robin" {
		upstream.Algorithm = nil
	}

	return upstream
}
