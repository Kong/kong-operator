package configuration_test

import (
	"testing"

	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKongUpstreamPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("sticky sessions validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1beta1.KongUpstreamPolicy]{
			{
				Name: "valid sticky sessions with hashOn.input=none",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("none")),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
			},
			{
				Name: "consistent-hashing with stickySessions should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("none")),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("spec.algorithm must be set to 'sticky-sessions' when spec.stickySessions is set."),
			},

			{
				Name: "sticky sessions without hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn but no input should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Header: new("X-Custom-Header"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input not 'none' should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("ip")),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but other hash fields should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:  new(configurationv1beta1.HashInput("none")),
							Header: new("X-Custom-Header"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but cookie field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:  new(configurationv1beta1.HashInput("none")),
							Cookie: new("hash-cookie"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but cookiePath field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:      new(configurationv1beta1.HashInput("none")),
							CookiePath: new("/path"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but uriCapture field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:      new(configurationv1beta1.HashInput("none")),
							URICapture: new("capture"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but queryArg field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:    new(configurationv1beta1.HashInput("none")),
							QueryArg: new("arg"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: new("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions without cookie should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("none")),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{},
					},
				},
				ExpectedErrorMessage: new("spec.stickySessions.cookie in body should be at least 1 chars long"),
			},
			{
				Name: "valid configuration without sticky sessions",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("round-robin"),
						Slots:     new(100),
					},
				},
			},
			{
				Name: "valid configuration with hashOn but no sticky sessions",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("ip")),
						},
					},
				},
			},
			{
				Name: "valid configuration with sticky-sessions algorithm and hashOn",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("none")),
						},
					},
				},
			},
			{
				Name: "invalid configuration with round-robin algorithm and hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("round-robin"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("ip")),
						},
					},
				},
				ExpectedErrorMessage: new("spec.algorithm must be set to either 'consistent-hashing' or 'sticky-sessions' when spec.hashOn is set."),
			},
			{
				Name: "invalid configuration with least-connections algorithm and hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("least-connections"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("ip")),
						},
					},
				},
				ExpectedErrorMessage: new("spec.algorithm must be set to either 'consistent-hashing' or 'sticky-sessions' when spec.hashOn is set."),
			},
			{
				Name: "invalid configuration with latency algorithm and hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("latency"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: new(configurationv1beta1.HashInput("ip")),
						},
					},
				},
				ExpectedErrorMessage: new("spec.algorithm must be set to either 'consistent-hashing' or 'sticky-sessions' when spec.hashOn is set."),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("consistent-hashing", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1beta1.KongUpstreamPolicy]{
			{
				Name: "hash on cookie with valid input",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie:     new("session-cookie-name"),
							CookiePath: new("/cookie-path"),
						},
					},
				},
			},
			{
				Name: "hash on cookie requires cookiePath field to be set",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie: new("session-cookie-name"),
						},
					},
				},
				ExpectedErrorMessage: new("When spec.hashOn.cookie is set, spec.hashOn.cookiePath is required."),
			},
			{
				Name: "hash on cookiePath requires cookie field to be set",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							CookiePath: new("/cookie-path"),
						},
					},
				},
				ExpectedErrorMessage: new("When spec.hashOn.cookiePath is set, spec.hashOn.cookie is required."),
			},
			{
				Name: "hash on header with valid input",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Header: new("X-Custom-Header"),
						},
					},
				},
			},
			{
				Name: "hash on header, hash on fallback cookie with valid input",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Header: new("X-Custom-Header"),
						},
						HashOnFallback: &configurationv1beta1.KongUpstreamHash{
							Cookie:     new("fallback-cookie"),
							CookiePath: new("/fallback-cookie-path"),
						},
					},
				},
			},
			{
				// NOTE: Per https://developer.konghq.com/gateway/entities/upstream/#consistent-hashing
				// > The hash_fallback setting is invalid and can’t be used if cookie is the primary hashing mechanism.
				Name: "hash on fallback (cookie) cannot be set when hash on cookie is set",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie:     new("cookie"),
							CookiePath: new("/cookie-path"),
						},
						HashOnFallback: &configurationv1beta1.KongUpstreamHash{
							Cookie:     new("fallback-cookie"),
							CookiePath: new("/fallback-cookie-path"),
						},
					},
				},
				ExpectedErrorMessage: new("spec.hashOnFallback must not be set when spec.hashOn.cookie is set."),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("healthchecks", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1beta1.KongUpstreamPolicy]{
			{
				Name: "healthchecks thresholds must be non-negative",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie:     new("cookie"),
							CookiePath: new("/cookie-path"),
						},
						Healthchecks: &configurationv1beta1.KongUpstreamHealthcheck{
							Threshold: new(-1),
						},
					},
				},
				ExpectedErrorMessage: new("Invalid value: -1: spec.healthchecks.threshold in body should be greater than or equal to 0"),
			},
			{
				Name: "healthchecks thresholds must be less than or equal to 100",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: new("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie:     new("cookie"),
							CookiePath: new("/cookie-path"),
						},
						Healthchecks: &configurationv1beta1.KongUpstreamHealthcheck{
							Threshold: new(101),
						},
					},
				},
				ExpectedErrorMessage: new("Invalid value: 101: spec.healthchecks.threshold in body should be less than or equal to 100"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
