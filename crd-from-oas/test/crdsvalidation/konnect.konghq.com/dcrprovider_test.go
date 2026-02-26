package configuration_test

import (
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/v2/crd-from-oas/test/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestDcrProvider(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := testscheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.DcrProvider]{
			{
				Name: "type Auth0 passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeAuth0,
							},
						},
					},
				},
			},
			{
				Name: "type AzureAd passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeAzureAd,
							},
						},
					},
				},
			},
			{
				Name: "type Curity passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeCurity,
							},
						},
					},
				},
			},
			{
				Name: "type Okta passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeOkta,
							},
						},
					},
				},
			},
			{
				Name: "type Http passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeHttp,
							},
						},
					},
				},
			},
			{
				Name: "invalid type fails validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: "InvalidType",
							},
						},
					},
				},
				ExpectedErrorMessage: new(`spec.apiSpec.type: Unsupported value: "InvalidType"`),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("Auth0 provider config", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.DcrProvider]{
			{
				Name: "Auth0 with full config passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeAuth0,
								Auth0: &konnectv1alpha1.CreateDcrProviderRequestAuth0{
									Name:         "auth0-provider",
									DisplayName:  "Auth0 Provider",
									Issuer:       "https://auth0.example.com",
									ProviderType: "auth0",
									DcrConfig: konnectv1alpha1.CreateDcrConfigAuth0InRequest{
										InitialClientID:       "client-id",
										InitialClientSecret:   "client-secret",
										InitialClientAudience: "https://api.example.com",
									},
									Labels: konnectv1alpha1.Labels{
										"env": "test",
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

	t.Run("Okta provider config", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.DcrProvider]{
			{
				Name: "Okta with full config passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeOkta,
								Okta: &konnectv1alpha1.CreateDcrProviderRequestOkta{
									Name:         "okta-provider",
									DisplayName:  "Okta Provider",
									Issuer:       "https://okta.example.com",
									ProviderType: "okta",
									DcrConfig: konnectv1alpha1.CreateDcrConfigOktaInRequest{
										DcrToken: "okta-dcr-token",
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

	t.Run("Http provider config", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.DcrProvider]{
			{
				Name: "Http with full config passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						APISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeHttp,
								Http: &konnectv1alpha1.CreateDcrProviderRequestHttp{
									Name:         "http-provider",
									DisplayName:  "HTTP Provider",
									Issuer:       "https://http.example.com",
									ProviderType: "http",
									DcrConfig: konnectv1alpha1.CreateDcrConfigHttpInRequest{
										APIKey:     "api-key",
										DcrBaseURL: "https://dcr.example.com",
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
