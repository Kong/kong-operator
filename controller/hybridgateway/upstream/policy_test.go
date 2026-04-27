package upstream

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
)

func TestTranslatePolicySpecToUpstreamAPISpec(t *testing.T) {
	t.Run("empty spec returns zero value", func(t *testing.T) {
		result := translatePolicySpecToUpstreamAPISpec(configurationv1beta1.KongUpstreamPolicySpec{})
		assert.Nil(t, result.Algorithm)
		assert.Nil(t, result.Slots)
		assert.Nil(t, result.HashOn)
		assert.Nil(t, result.Healthchecks)
	})

	t.Run("algorithm is set", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: ptr.To("least-connections"),
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.Algorithm)
		assert.Equal(t, sdkkonnectcomp.UpstreamAlgorithm("least-connections"), *result.Algorithm)
	})

	t.Run("slots is set", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Slots: ptr.To(512),
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.Slots)
		assert.Equal(t, int64(512), *result.Slots)
	})

	t.Run("hash_on input", func(t *testing.T) {
		input := configurationv1beta1.HashInput("ip")
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: ptr.To("consistent-hashing"),
			HashOn:    &configurationv1beta1.KongUpstreamHash{Input: &input},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashOn)
		assert.Equal(t, sdkkonnectcomp.HashOn("ip"), *result.HashOn)
	})

	t.Run("hash_on header", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: ptr.To("consistent-hashing"),
			HashOn:    &configurationv1beta1.KongUpstreamHash{Header: ptr.To("X-User-ID")},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashOn)
		assert.Equal(t, sdkkonnectcomp.HashOnHeader, *result.HashOn)
		assert.Equal(t, ptr.To("X-User-ID"), result.HashOnHeader)
	})

	t.Run("hash_on cookie", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: ptr.To("consistent-hashing"),
			HashOn: &configurationv1beta1.KongUpstreamHash{
				Cookie:     ptr.To("session"),
				CookiePath: ptr.To("/api"),
			},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashOn)
		assert.Equal(t, sdkkonnectcomp.HashOnCookie, *result.HashOn)
		assert.Equal(t, ptr.To("session"), result.HashOnCookie)
		assert.Equal(t, ptr.To("/api"), result.HashOnCookiePath)
	})

	t.Run("hash_on query_arg", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: ptr.To("consistent-hashing"),
			HashOn:    &configurationv1beta1.KongUpstreamHash{QueryArg: ptr.To("user_id")},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashOn)
		assert.Equal(t, sdkkonnectcomp.HashOnQueryArg, *result.HashOn)
		assert.Equal(t, ptr.To("user_id"), result.HashOnQueryArg)
	})

	t.Run("hash_on uri_capture", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: ptr.To("consistent-hashing"),
			HashOn:    &configurationv1beta1.KongUpstreamHash{URICapture: ptr.To("group1")},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashOn)
		assert.Equal(t, sdkkonnectcomp.HashOnURICapture, *result.HashOn)
		assert.Equal(t, ptr.To("group1"), result.HashOnURICapture)
	})

	t.Run("hash_fallback header", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm:      ptr.To("consistent-hashing"),
			HashOnFallback: &configurationv1beta1.KongUpstreamHash{Header: ptr.To("X-Fallback")},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashFallback)
		assert.Equal(t, sdkkonnectcomp.HashFallbackHeader, *result.HashFallback)
		assert.Equal(t, ptr.To("X-Fallback"), result.HashFallbackHeader)
	})

	t.Run("hash_fallback query_arg", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm:      ptr.To("consistent-hashing"),
			HashOnFallback: &configurationv1beta1.KongUpstreamHash{QueryArg: ptr.To("fb_key")},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashFallback)
		assert.Equal(t, sdkkonnectcomp.HashFallbackQueryArg, *result.HashFallback)
		assert.Equal(t, ptr.To("fb_key"), result.HashFallbackQueryArg)
	})

	t.Run("hash_fallback input", func(t *testing.T) {
		input := configurationv1beta1.HashInput("consumer")
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm:      ptr.To("consistent-hashing"),
			HashOnFallback: &configurationv1beta1.KongUpstreamHash{Input: &input},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.HashFallback)
		assert.Equal(t, sdkkonnectcomp.HashFallback("consumer"), *result.HashFallback)
	})

	t.Run("sticky sessions", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: ptr.To("sticky-sessions"),
			StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
				Cookie:     "sticky",
				CookiePath: ptr.To("/"),
			},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		assert.Equal(t, ptr.To("sticky"), result.StickySessionsCookie)
		assert.Equal(t, ptr.To("/"), result.StickySessionsCookiePath)
	})

	t.Run("healthchecks active", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Healthchecks: &configurationv1beta1.KongUpstreamHealthcheck{
				Active: &configurationv1beta1.KongUpstreamActiveHealthcheck{
					Concurrency: ptr.To(5),
					HTTPPath:    ptr.To("/health"),
					Timeout:     ptr.To(3),
					Healthy: &configurationv1beta1.KongUpstreamHealthcheckHealthy{
						HTTPStatuses: []configurationv1beta1.HTTPStatus{200, 201},
						Successes:    ptr.To(2),
						Interval:     ptr.To(10),
					},
					Unhealthy: &configurationv1beta1.KongUpstreamHealthcheckUnhealthy{
						HTTPStatuses: []configurationv1beta1.HTTPStatus{500},
						HTTPFailures: ptr.To(3),
						TCPFailures:  ptr.To(1),
						Timeouts:     ptr.To(2),
						Interval:     ptr.To(5),
					},
				},
				Threshold: ptr.To(50),
			},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.Healthchecks)
		require.NotNil(t, result.Healthchecks.Active)

		active := result.Healthchecks.Active
		require.NotNil(t, active.Concurrency)
		assert.Equal(t, int64(5), *active.Concurrency)
		assert.Equal(t, ptr.To("/health"), active.HTTPPath)
		require.NotNil(t, active.Timeout)
		assert.InDelta(t, float64(3), *active.Timeout, 0)

		require.NotNil(t, active.Healthy)
		assert.Equal(t, []int64{200, 201}, active.Healthy.HTTPStatuses)
		require.NotNil(t, active.Healthy.Successes)
		assert.Equal(t, int64(2), *active.Healthy.Successes)
		require.NotNil(t, active.Healthy.Interval)
		assert.InDelta(t, float64(10), *active.Healthy.Interval, 0)

		require.NotNil(t, active.Unhealthy)
		assert.Equal(t, []int64{500}, active.Unhealthy.HTTPStatuses)
		require.NotNil(t, active.Unhealthy.HTTPFailures)
		assert.Equal(t, int64(3), *active.Unhealthy.HTTPFailures)
		require.NotNil(t, active.Unhealthy.TCPFailures)
		assert.Equal(t, int64(1), *active.Unhealthy.TCPFailures)
		require.NotNil(t, active.Unhealthy.Timeouts)
		assert.Equal(t, int64(2), *active.Unhealthy.Timeouts)
		require.NotNil(t, active.Unhealthy.Interval)
		assert.InDelta(t, float64(5), *active.Unhealthy.Interval, 0)

		require.NotNil(t, result.Healthchecks.Threshold)
		assert.InDelta(t, float64(50), *result.Healthchecks.Threshold, 0)
	})

	t.Run("healthchecks passive", func(t *testing.T) {
		spec := configurationv1beta1.KongUpstreamPolicySpec{
			Healthchecks: &configurationv1beta1.KongUpstreamHealthcheck{
				Passive: &configurationv1beta1.KongUpstreamPassiveHealthcheck{
					Type: ptr.To("http"),
					Healthy: &configurationv1beta1.KongUpstreamHealthcheckHealthy{
						HTTPStatuses: []configurationv1beta1.HTTPStatus{200},
						Successes:    ptr.To(1),
					},
					Unhealthy: &configurationv1beta1.KongUpstreamHealthcheckUnhealthy{
						HTTPStatuses: []configurationv1beta1.HTTPStatus{503},
						HTTPFailures: ptr.To(2),
					},
				},
			},
		}
		result := translatePolicySpecToUpstreamAPISpec(spec)
		require.NotNil(t, result.Healthchecks)
		require.NotNil(t, result.Healthchecks.Passive)

		passive := result.Healthchecks.Passive
		require.NotNil(t, passive.Type)
		assert.Equal(t, sdkkonnectcomp.UpstreamHealthchecksType("http"), *passive.Type)

		require.NotNil(t, passive.Healthy)
		assert.Equal(t, []int64{200}, passive.Healthy.HTTPStatuses)

		require.NotNil(t, passive.Unhealthy)
		assert.Equal(t, []int64{503}, passive.Unhealthy.HTTPStatuses)
		require.NotNil(t, passive.Unhealthy.HTTPFailures)
		assert.Equal(t, int64(2), *passive.Unhealthy.HTTPFailures)
	})
}
