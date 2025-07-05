package configuration_test

import (
	"testing"

	"github.com/samber/lo"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestKongUpstreamPolicy(t *testing.T) {
	t.Run("sticky sessions validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1beta1.KongUpstreamPolicy]{
			{
				Name: "valid sticky sessions with hashOn.input=none",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("none")),
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("none")),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.algorithm must be set to 'sticky-sessions' when spec.stickySessions is set."),
			},

			{
				Name: "sticky sessions without hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn but no input should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Header: lo.ToPtr("X-Custom-Header"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input not 'none' should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("ip")),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but other hash fields should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:  lo.ToPtr(configurationv1beta1.HashInput("none")),
							Header: lo.ToPtr("X-Custom-Header"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but cookie field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:  lo.ToPtr(configurationv1beta1.HashInput("none")),
							Cookie: lo.ToPtr("hash-cookie"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but cookiePath field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:      lo.ToPtr(configurationv1beta1.HashInput("none")),
							CookiePath: lo.ToPtr("/path"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but uriCapture field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:      lo.ToPtr(configurationv1beta1.HashInput("none")),
							URICapture: lo.ToPtr("capture"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions with hashOn.input=none but queryArg field should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input:    lo.ToPtr(configurationv1beta1.HashInput("none")),
							QueryArg: lo.ToPtr("arg"),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
							Cookie: "session-cookie",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.stickySessions is set, spec.hashOn.input must be set to 'none' (no other hash_on fields allowed)."),
			},
			{
				Name: "sticky sessions without cookie should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("none")),
						},
						StickySessions: &configurationv1beta1.KongUpstreamStickySessions{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.stickySessions.cookie in body should be at least 1 chars long"),
			},
			{
				Name: "valid configuration without sticky sessions",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("round-robin"),
						Slots:     lo.ToPtr(100),
					},
				},
			},
			{
				Name: "valid configuration with hashOn but no sticky sessions",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("ip")),
						},
					},
				},
			},
			{
				Name: "valid configuration with sticky-sessions algorithm and hashOn",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("sticky-sessions"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("none")),
						},
					},
				},
			},
			{
				Name: "invalid configuration with round-robin algorithm and hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("round-robin"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("ip")),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.algorithm must be set to either 'consistent-hashing' or 'sticky-sessions' when spec.hashOn is set."),
			},
			{
				Name: "invalid configuration with least-connections algorithm and hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("least-connections"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("ip")),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.algorithm must be set to either 'consistent-hashing' or 'sticky-sessions' when spec.hashOn is set."),
			},
			{
				Name: "invalid configuration with latency algorithm and hashOn should fail",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("latency"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Input: lo.ToPtr(configurationv1beta1.HashInput("ip")),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.algorithm must be set to either 'consistent-hashing' or 'sticky-sessions' when spec.hashOn is set."),
			},
		}.Run(t)
	})

	t.Run("consistent-hashing", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1beta1.KongUpstreamPolicy]{
			{
				Name: "hash on cookie with valid input",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie:     lo.ToPtr("session-cookie-name"),
							CookiePath: lo.ToPtr("/cookie-path"),
						},
					},
				},
			},
			{
				Name: "hash on cookie requires cookiePath field to be set",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie: lo.ToPtr("session-cookie-name"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.hashOn.cookie is set, spec.hashOn.cookiePath is required."),
			},
			{
				Name: "hash on cookiePath requires cookie field to be set",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							CookiePath: lo.ToPtr("/cookie-path"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("When spec.hashOn.cookiePath is set, spec.hashOn.cookie is required."),
			},
			{
				Name: "hash on header with valid input",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Header: lo.ToPtr("X-Custom-Header"),
						},
					},
				},
			},
			{
				Name: "hash on header, hash on fallback cookie with valid input",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Header: lo.ToPtr("X-Custom-Header"),
						},
						HashOnFallback: &configurationv1beta1.KongUpstreamHash{
							Cookie:     lo.ToPtr("fallback-cookie"),
							CookiePath: lo.ToPtr("/fallback-cookie-path"),
						},
					},
				},
			},
			{
				// NOTE: Per https://developer.konghq.com/gateway/entities/upstream/#consistent-hashing
				// > The hash_fallback setting is invalid and can’t be used if cookie is the primary hashing mechanism.
				Name: "hash on fallback (cookie) cannot be set when hash on cookie is set",
				TestObject: &configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1beta1.KongUpstreamPolicySpec{
						Algorithm: lo.ToPtr("consistent-hashing"),
						HashOn: &configurationv1beta1.KongUpstreamHash{
							Cookie:     lo.ToPtr("cookie"),
							CookiePath: lo.ToPtr("/cookie-path"),
						},
						HashOnFallback: &configurationv1beta1.KongUpstreamHash{
							Cookie:     lo.ToPtr("fallback-cookie"),
							CookiePath: lo.ToPtr("/fallback-cookie-path"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.hashOnFallback must not be set when spec.hashOn.cookie is set."),
			},
		}.Run(t)
	})
}
