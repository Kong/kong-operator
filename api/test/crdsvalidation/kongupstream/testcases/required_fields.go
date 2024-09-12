package testcases

import (
	"github.com/Kong/sdk-konnect-go/models/components"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/samber/lo"
)

var requiredFields = testCasesGroup{
	Name: "required fields validation",
	TestCases: []testCase{
		{
			Name: "hash_fallback_header is required when hash_fallback is set to 'header'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback:       lo.ToPtr(components.HashFallbackHeader),
						HashFallbackHeader: lo.ToPtr("X-Hash-Fallback"),
					},
				},
			},
		},
		{
			Name: "validation fails when hash_fallback_header is not provided when hash_fallback is set to 'header'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback: lo.ToPtr(components.HashFallbackHeader),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_fallback_header is required when `hash_fallback` is set to `header`"),
		},
		{
			Name: "hash_fallback_query_arg is required when hash_fallback is set to 'query_arg'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback:         lo.ToPtr(components.HashFallbackQueryArg),
						HashFallbackQueryArg: lo.ToPtr("arg"),
					},
				},
			},
		},
		{
			Name: "validation fails when hash_fallback_query_arg is not provided when hash_fallback is set to 'query_arg'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback: lo.ToPtr(components.HashFallbackQueryArg),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_fallback_query_arg is required when `hash_fallback` is set to `query_arg`"),
		},
		{
			Name: "hash_fallback_uri_capture is required when hash_fallback is set to 'uri_capture'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback:           lo.ToPtr(components.HashFallbackURICapture),
						HashFallbackURICapture: lo.ToPtr("arg"),
					},
				},
			},
		},
		{
			Name: "validation fails when hash_fallback_uri_capture is not provided when hash_fallback is set to 'uri_capture'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback: lo.ToPtr(components.HashFallbackURICapture),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_fallback_uri_capture is required when `hash_fallback` is set to `uri_capture`"),
		},
		{
			Name: "hash_on_cookie and hash_on_cookie_path are required when hash_on is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn:           lo.ToPtr(components.HashOnCookie),
						HashOnCookie:     lo.ToPtr("cookie"),
						HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
					},
				},
			},
		},
		{
			Name: "hash_on_cookie and hash_on_cookie_path are required when hash_fallback is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback:     lo.ToPtr(components.HashFallbackCookie),
						HashOnCookie:     lo.ToPtr("cookie"),
						HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
					},
				},
			},
		},
		{
			Name: "validation fails when hash_on_cookie is not provided when hash_on is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn:           lo.ToPtr(components.HashOnCookie),
						HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("hash_on_cookie is required when hash_on is set to `cookie`."),
		},
		{
			Name: "validation fails when hash_on_cookie is not provided when hash_fallback is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback:     lo.ToPtr(components.HashFallbackCookie),
						HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("hash_on_cookie is required when hash_fallback is set to `cookie`."),
		},
		{
			Name: "validation fails when hash_on_cookie_path is not provided when hash_on is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn:       lo.ToPtr(components.HashOnCookie),
						HashOnCookie: lo.ToPtr("cookie"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_on is set to `cookie`."),
		},
		{
			Name: "validation fails when hash_on_cookie_path is not provided when hash_fallback is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback: lo.ToPtr(components.HashFallbackCookie),
						HashOnCookie: lo.ToPtr("cookie"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_fallback is set to `cookie`."),
		},
		{
			Name: "validation fails when hash_on_cookie_path nor hash_on_cookie are provided when hash_fallback is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashFallback: lo.ToPtr(components.HashFallbackCookie),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_fallback is set to `cookie`."),
		},
		{
			Name: "validation fails when hash_on_cookie_path nor hash_on_cookie are provided when hash_on is set to 'cookie'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn: lo.ToPtr(components.HashOnCookie),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_on is set to `cookie`."),
		},
		{
			Name: "validation fails when hash_on_header is not provided when hash_on is set to 'header'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn: lo.ToPtr(components.HashOnHeader),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_on_header is required when hash_on is set to `header`"),
		},
		{
			Name: "hash_on_query_arg is required when hash_on is set to 'query_arg'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn:         lo.ToPtr(components.HashOnQueryArg),
						HashOnQueryArg: lo.ToPtr("arg"),
					},
				},
			},
		},
		{
			Name: "validation fails when hash_on_query_arg is not provided when hash_on is set to 'query_arg'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn: lo.ToPtr(components.HashOnQueryArg),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_on_query_arg is required when `hash_on` is set to `query_arg`"),
		},
		{
			Name: "hash_on_uri_capture is required when hash_on is set to 'uri_capture'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn:           lo.ToPtr(components.HashOnURICapture),
						HashOnURICapture: lo.ToPtr("arg"),
					},
				},
			},
		},
		{
			Name: "validation fails when hash_on_uri_capture is not provided when hash_on is set to 'uri_capture'",
			KongUpstream: configurationv1alpha1.KongUpstream{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						HashOn: lo.ToPtr(components.HashOnURICapture),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_on_uri_capture is required when `hash_on` is set to `uri_capture`"),
		},
	},
}
