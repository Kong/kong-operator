package configuration_test

import (
	"strings"
	"testing"

	"github.com/samber/lo"

	konnectv1alpha1 "github.com/kong/kong-operator/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/crd-from-oas/test/scheme"
	"github.com/kong/kong-operator/test/envtest"
)

func TestIdentityProviderRequest(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := testscheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.IdentityProviderRequest]{
			{
				Name: "type oidc passes validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type: "oidc",
						},
					},
				},
			},
			{
				Name: "type saml passes validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type: "saml",
						},
					},
				},
			},
			{
				Name: "invalid type fails validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type: "invalid",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr(`spec.apiSpec.type: Unsupported value: "invalid"`),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("login_path field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.IdentityProviderRequest]{
			{
				Name: "login_path with valid value passes validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type:      "oidc",
							LoginPath: "/login",
						},
					},
				},
			},
			{
				Name: "login_path at max length (256) passes validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type:      "oidc",
							LoginPath: konnectv1alpha1.IdentityProviderLoginPath(strings.Repeat("p", 256)),
						},
					},
				},
			},
			{
				Name: "login_path exceeding max length (257) fails validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type:      "oidc",
							LoginPath: konnectv1alpha1.IdentityProviderLoginPath(strings.Repeat("p", 257)),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.apiSpec.login_path: Too long: may not be more than 256"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("config field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.IdentityProviderRequest]{
			{
				Name: "config with OIDC type passes validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type: "oidc",
							Config: &konnectv1alpha1.Config{
								Type: konnectv1alpha1.ConfigTypeOIDC,
								OIDC: &konnectv1alpha1.ConfigureOIDCIdentityProviderConfig{
									ClientID:     "my-client-id",
									ClientSecret: "my-client-secret",
									IssuerURL:    "https://issuer.example.com",
									Scopes:       []string{"openid", "profile"},
									ClaimMappings: konnectv1alpha1.OIDCIdentityProviderClaimMappings{
										Email:  "email",
										Name:   "name",
										Groups: "groups",
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "config with SAML type passes validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type: "saml",
							Config: &konnectv1alpha1.Config{
								Type: konnectv1alpha1.ConfigTypeSAML,
								SAML: &konnectv1alpha1.SAMLIdentityProviderConfig{
									IdpMetadataURL: "https://idp.example.com/metadata",
									IdpMetadataXML: "<xml>metadata</xml>",
								},
							},
						},
					},
				},
			},
			{
				Name: "config with invalid type fails validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Config: &konnectv1alpha1.Config{
								Type: "InvalidConfigType",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr(`spec.apiSpec.config.type: Unsupported value: "InvalidConfigType"`),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("full spec with all fields passes validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.IdentityProviderRequest]{
			{
				Name: "all fields populated passes validation",
				TestObject: &konnectv1alpha1.IdentityProviderRequest{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.IdentityProviderRequestSpec{
						APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
							Type:      "oidc",
							Enabled:   true,
							LoginPath: "/sso/login",
							Config: &konnectv1alpha1.Config{
								Type: konnectv1alpha1.ConfigTypeOIDC,
								OIDC: &konnectv1alpha1.ConfigureOIDCIdentityProviderConfig{
									ClientID:     "full-spec-client-id",
									ClientSecret: "full-spec-client-secret",
									IssuerURL:    "https://issuer.example.com",
									Scopes:       []string{"openid", "profile", "email"},
									ClaimMappings: konnectv1alpha1.OIDCIdentityProviderClaimMappings{
										Email:  "email",
										Name:   "name",
										Groups: "groups",
									},
								},
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
