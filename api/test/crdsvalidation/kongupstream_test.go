package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

func TestKongUpstream(t *testing.T) {
	obj := &configurationv1alpha1.KongUpstream{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongUpstream",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: commonObjectMeta,
	}

	t.Run("cp ref", func(t *testing.T) {
		NewCRDValidationTestCasesGroupCPRefChange(t, obj, NotSupportedByKIC, ControlPlaneRefRequired).Run(t)
	})

	t.Run("cp ref, type=kic", func(t *testing.T) {
		NewCRDValidationTestCasesGroupCPRefChangeKICUnsupportedTypes(t, obj, EmptyControlPlaneRefNotAllowed).Run(t)
	})

	t.Run("required fields", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongUpstream]{
			{
				Name: "hash_fallback_header is required when hash_fallback is set to 'header'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback:       lo.ToPtr(sdkkonnectcomp.HashFallbackHeader),
							HashFallbackHeader: lo.ToPtr("X-Hash-Fallback"),
						},
					},
				},
			},
			{
				Name: "validation fails when hash_fallback_header is not provided when hash_fallback is set to 'header'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback: lo.ToPtr(sdkkonnectcomp.HashFallbackHeader),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_fallback_header is required when `hash_fallback` is set to `header`"),
			},
			{
				Name: "hash_fallback_query_arg is required when hash_fallback is set to 'query_arg'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback:         lo.ToPtr(sdkkonnectcomp.HashFallbackQueryArg),
							HashFallbackQueryArg: lo.ToPtr("arg"),
						},
					},
				},
			},
			{
				Name: "validation fails when hash_fallback_query_arg is not provided when hash_fallback is set to 'query_arg'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback: lo.ToPtr(sdkkonnectcomp.HashFallbackQueryArg),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_fallback_query_arg is required when `hash_fallback` is set to `query_arg`"),
			},
			{
				Name: "hash_fallback_uri_capture is required when hash_fallback is set to 'uri_capture'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback:           lo.ToPtr(sdkkonnectcomp.HashFallbackURICapture),
							HashFallbackURICapture: lo.ToPtr("arg"),
						},
					},
				},
			},
			{
				Name: "validation fails when hash_fallback_uri_capture is not provided when hash_fallback is set to 'uri_capture'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback: lo.ToPtr(sdkkonnectcomp.HashFallbackURICapture),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_fallback_uri_capture is required when `hash_fallback` is set to `uri_capture`"),
			},
			{
				Name: "hash_on_cookie and hash_on_cookie_path are required when hash_on is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:           lo.ToPtr(sdkkonnectcomp.HashOnCookie),
							HashOnCookie:     lo.ToPtr("cookie"),
							HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
						},
					},
				},
			},
			{
				Name: "hash_on_cookie and hash_on_cookie_path are required when hash_fallback is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback:     lo.ToPtr(sdkkonnectcomp.HashFallbackCookie),
							HashOnCookie:     lo.ToPtr("cookie"),
							HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
						},
					},
				},
			},
			{
				Name: "validation fails when hash_on_cookie is not provided when hash_on is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:           lo.ToPtr(sdkkonnectcomp.HashOnCookie),
							HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("hash_on_cookie is required when hash_on is set to `cookie`."),
			},
			{
				Name: "validation fails when hash_on_cookie is not provided when hash_fallback is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback:     lo.ToPtr(sdkkonnectcomp.HashFallbackCookie),
							HashOnCookiePath: lo.ToPtr("X-Hash-On-Cookie-Path"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("hash_on_cookie is required when hash_fallback is set to `cookie`."),
			},
			{
				Name: "validation fails when hash_on_cookie_path is not provided when hash_on is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:       lo.ToPtr(sdkkonnectcomp.HashOnCookie),
							HashOnCookie: lo.ToPtr("cookie"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_on is set to `cookie`."),
			},
			{
				Name: "validation fails when hash_on_cookie_path is not provided when hash_fallback is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback: lo.ToPtr(sdkkonnectcomp.HashFallbackCookie),
							HashOnCookie: lo.ToPtr("cookie"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_fallback is set to `cookie`."),
			},
			{
				Name: "validation fails when hash_on_cookie_path nor hash_on_cookie are provided when hash_fallback is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashFallback: lo.ToPtr(sdkkonnectcomp.HashFallbackCookie),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_fallback is set to `cookie`."),
			},
			{
				Name: "validation fails when hash_on_cookie_path nor hash_on_cookie are provided when hash_on is set to 'cookie'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn: lo.ToPtr(sdkkonnectcomp.HashOnCookie),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("hash_on_cookie_path is required when hash_on is set to `cookie`."),
			},
			{
				Name: "validation fails when hash_on_header is not provided when hash_on is set to 'header'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn: lo.ToPtr(sdkkonnectcomp.HashOnHeader),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_on_header is required when hash_on is set to `header`"),
			},
			{
				Name: "hash_on_query_arg is required when hash_on is set to 'query_arg'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:         lo.ToPtr(sdkkonnectcomp.HashOnQueryArg),
							HashOnQueryArg: lo.ToPtr("arg"),
						},
					},
				},
			},
			{
				Name: "validation fails when hash_on_query_arg is not provided when hash_on is set to 'query_arg'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn: lo.ToPtr(sdkkonnectcomp.HashOnQueryArg),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_on_query_arg is required when `hash_on` is set to `query_arg`"),
			},
			{
				Name: "hash_on_uri_capture is required when hash_on is set to 'uri_capture'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:           lo.ToPtr(sdkkonnectcomp.HashOnURICapture),
							HashOnURICapture: lo.ToPtr("arg"),
						},
					},
				},
			},
			{
				Name: "validation fails when hash_on_uri_capture is not provided when hash_on is set to 'uri_capture'",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn: lo.ToPtr(sdkkonnectcomp.HashOnURICapture),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": hash_on_uri_capture is required when `hash_on` is set to `uri_capture`"),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongUpstream]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:         lo.ToPtr(sdkkonnectcomp.HashOnQueryArg),
							HashOnQueryArg: lo.ToPtr("arg"),
							Tags: func() []string {
								var tags []string
								for i := range 20 {
									tags = append(tags, fmt.Sprintf("tag-%d", i))
								}
								return tags
							}(),
						},
					},
				},
			},
			{
				Name: "more than 20 tags are not allowed",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:         lo.ToPtr(sdkkonnectcomp.HashOnQueryArg),
							HashOnQueryArg: lo.ToPtr("arg"),
							Tags: func() []string {
								var tags []string
								for i := range 21 {
									tags = append(tags, fmt.Sprintf("tag-%d", i))
								}
								return tags
							}(),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.tags: Too many: 21: must have at most 20 items"),
			},
			{
				Name: "tags entries must not be longer than 128 characters",
				TestObject: &configurationv1alpha1.KongUpstream{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongUpstreamSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							HashOn:         lo.ToPtr(sdkkonnectcomp.HashOnQueryArg),
							HashOnQueryArg: lo.ToPtr("arg"),
							Tags: []string{
								lo.RandomString(129, lo.AlphanumericCharset),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("tags entries must not be longer than 128 characters"),
			},
		}.Run(t)
	})
}
