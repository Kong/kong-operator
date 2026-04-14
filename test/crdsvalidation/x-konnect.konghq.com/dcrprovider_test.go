package configuration_test

import (
	"strings"
	"testing"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	xkonnectv1alpha1 "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestDcrProvider(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := Scheme(t)
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("auth0 labels field validation", func(t *testing.T) {
		dcrProviderWithLabelValue := func(labelValue string) *xkonnectv1alpha1.DcrProvider {
			return &xkonnectv1alpha1.DcrProvider{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: xkonnectv1alpha1.DcrProviderSpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
							Name: "test-auth",
						},
					},
					APISpec: xkonnectv1alpha1.DcrProviderAPISpec{
						DcrProviderConfig: &xkonnectv1alpha1.DcrProviderConfig{
							Type: xkonnectv1alpha1.DcrProviderConfigTypeAuth0,
							Auth0: &xkonnectv1alpha1.CreateDcrProviderRequestAuth0{
								DcrConfig: xkonnectv1alpha1.CreateDcrConfigAuth0InRequest{
									InitialClientID:     "client-id",
									InitialClientSecret: "client-secret",
								},
								Issuer:       "https://example.com",
								Labels:       xkonnectv1alpha1.Labels{"team": xkonnectv1alpha1.LabelsValue(labelValue)},
								Name:         "auth0-dcr-provider",
								ProviderType: "auth0",
							},
						},
					},
				},
			}
		}

		common.TestCasesGroup[*xkonnectv1alpha1.DcrProvider]{
			{
				Name:       "labels value at max length (63) passes validation",
				TestObject: dcrProviderWithLabelValue(strings.Repeat("a", 63)),
			},
			{
				Name:                 "labels value exceeding max length (64) fails validation",
				TestObject:           dcrProviderWithLabelValue(strings.Repeat("a", 64)),
				ExpectedErrorMessage: new("Too long: may not be"),
			},
			{
				Name:                 "labels value with invalid pattern fails validation",
				TestObject:           dcrProviderWithLabelValue("invalid!"),
				ExpectedErrorMessage: new("^[a-z0-9A-Z]{1}([a-z0-9A-Z-._]*[a-z0-9A-Z]+)?$"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
