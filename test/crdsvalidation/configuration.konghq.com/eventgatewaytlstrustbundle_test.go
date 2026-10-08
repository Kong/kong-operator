package configuration_test

import (
	"testing"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validTLSTrustBundle(ns string) *configurationv1alpha1.EventGatewayTLSTrustBundle {
	return &configurationv1alpha1.EventGatewayTLSTrustBundle{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: configurationv1alpha1.EventGatewayTLSTrustBundleSpec{
			GatewayRef: generatedParentRef(),
			APISpec: configurationv1alpha1.EventGatewayTLSTrustBundleAPISpec{
				Name: "trust-bundle",
				Config: configurationv1alpha1.TLSTrustBundleConfig{
					TrustedCa: inlineSDS("ca-pem-data"),
				},
			},
		},
	}
}

func TestEventGatewayTLSTrustBundle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("trustedCa SensitiveDataSource validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayTLSTrustBundle]{
			{
				Name:       "inline type with value passes",
				TestObject: validTLSTrustBundle(ns.Name),
			},
			{
				Name: "secretRef type with secretRef passes",
				TestObject: func() *configurationv1alpha1.EventGatewayTLSTrustBundle {
					obj := validTLSTrustBundle(ns.Name)
					obj.Spec.APISpec.Config.TrustedCa = secretRefSDS("my-ca-secret", "ca.crt")
					return obj
				}(),
			},
			{
				Name: "inline type without value fails",
				TestObject: func() *configurationv1alpha1.EventGatewayTLSTrustBundle {
					obj := validTLSTrustBundle(ns.Name)
					obj.Spec.APISpec.Config.TrustedCa = configurationv1alpha1.SensitiveDataSource{
						Type: configurationv1alpha1.SensitiveDataSourceTypeInline,
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
			{
				Name: "secretRef type without secretRef fails",
				TestObject: func() *configurationv1alpha1.EventGatewayTLSTrustBundle {
					obj := validTLSTrustBundle(ns.Name)
					obj.Spec.APISpec.Config.TrustedCa = configurationv1alpha1.SensitiveDataSource{
						Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("name validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayTLSTrustBundle]{
			{
				Name: "missing name fails",
				TestObject: func() *configurationv1alpha1.EventGatewayTLSTrustBundle {
					obj := validTLSTrustBundle(ns.Name)
					obj.Spec.APISpec.Name = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.name: Required value"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
