package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	common "github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestKonnectAPIAuthConfiguration(t *testing.T) {
	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectAPIAuthConfiguration]{
			{
				Name: "valid token type - spat prefix",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "spat_token",
						ServerURL: "api.us.konghq.com",
					},
				},
			},
			{
				Name: "valid token type - kpat prefix",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "kpat_token",
						ServerURL: "api.us.konghq.com",
					},
				},
			},
			{
				Name: "invalid token type - no prefix",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "token",
						ServerURL: "api.us.konghq.com",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Konnect tokens have to start with spat_ or kpat_"),
			},
			{
				Name: "invalid token type - missing token",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						ServerURL: "api.us.konghq.com",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Konnect tokens have to start with spat_ or kpat_"),
			},
			{
				Name: "token is required once set",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "kpat_token",
						ServerURL: "api.us.konghq.com",
					},
				},
				Update: func(obj *konnectv1alpha1.KonnectAPIAuthConfiguration) {
					obj.Spec.Token = ""
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("Token is required once set"),
			},
			{
				Name: "valid secretRef type",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
						SecretRef: &corev1.SecretReference{
							Name: "secret",
						},
						ServerURL: "api.us.konghq.com",
					},
				},
			},
			{
				Name: "invalid secretRef type - missing secretRef",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
						ServerURL: "api.us.konghq.com",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.secretRef is required if auth type is set to secretRef"),
			},
			{
				Name: "server URL is required",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:  konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token: "spat_token",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Server URL is required"),
			},
			{
				Name: "server URL is immutable",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "spat_token",
						ServerURL: "api.us.konghq.com",
					},
				},
				Update: func(obj *konnectv1alpha1.KonnectAPIAuthConfiguration) {
					obj.Spec.ServerURL = "api.eu.konghq.com"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("Server URL is immutable"),
			},
			{
				Name: "server URL is valid when using HTTPs scheme",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "spat_token",
						ServerURL: "https://api.us.konghq.com",
					},
				},
			},
			{
				Name: "server URL must use HTTPs if specifying scheme",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "spat_token",
						ServerURL: "http://api.us.konghq.com",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Server URL must use HTTPs if specifying scheme"),
			},
			{
				Name: "server URL must satisfy hostname (RFC 1123) regex if not a valid absolute URL",
				TestObject: &konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
						Token:     "spat_token",
						ServerURL: "%%%invalid%%%hostname",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Server URL must satisfy hostname (RFC 1123) regex if not a valid absolute URL"),
			},
		}.Run(t)
	})
}
