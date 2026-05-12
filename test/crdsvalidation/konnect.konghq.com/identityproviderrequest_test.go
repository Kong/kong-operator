package crdsvalidation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestIdentityProviderRequest(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	validOIDCIdentityProviderRequest := func() *konnectv1alpha1.IdentityProviderRequest {
		return &konnectv1alpha1.IdentityProviderRequest{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.IdentityProviderRequestSpec{
				PortalRef: commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "portal-name",
					},
				},
				APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
					Type: konnectv1alpha1.IdentityProviderType("oidc"),
					Config: &konnectv1alpha1.IdentityProviderRequestConfig{
						Type: konnectv1alpha1.IdentityProviderRequestConfigTypeOIDC,
						OIDC: &konnectv1alpha1.OIDCIdentityProviderConfig{
							ClientID:  "client-id",
							IssuerURL: "https://issuer.example.com",
						},
					},
				},
			},
		}
	}

	t.Run("OIDC config validation and defaults", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.IdentityProviderRequest]{
			{
				Name:       "valid OIDC identity provider request passes validation",
				TestObject: validOIDCIdentityProviderRequest(),
				Assert: func(t *testing.T, obj *konnectv1alpha1.IdentityProviderRequest) {
					require.NotNil(t, obj.Spec.APISpec.Config)
					require.NotNil(t, obj.Spec.APISpec.Config.OIDC)
				},
			},
			{
				Name: "missing OIDC client_id fails validation",
				TestObject: func() *konnectv1alpha1.IdentityProviderRequest {
					obj := validOIDCIdentityProviderRequest()
					obj.Spec.APISpec.Config.OIDC.ClientID = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.config.oIDC.clientID"),
			},
			{
				Name: "invalid identity provider type fails validation",
				TestObject: func() *konnectv1alpha1.IdentityProviderRequest {
					obj := validOIDCIdentityProviderRequest()
					obj.Spec.APISpec.Type = konnectv1alpha1.IdentityProviderType("ldap")
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.type"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
