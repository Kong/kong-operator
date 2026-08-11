package configuration_test

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func generatedParentRef() commonv1alpha1.ObjectRef {
	return commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: "my-event-gateway",
		},
	}
}

func TestGeneratedParentRefImmutability(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("EventGatewayDataPlaneCertificate", func(t *testing.T) {
		obj := &configurationv1alpha1.EventGatewayDataPlaneCertificate{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{
				GatewayRef: generatedParentRef(),
				APISpec: configurationv1alpha1.EventGatewayDataPlaneCertificateAPISpec{
					Certificate: configurationv1alpha1.SensitiveDataSource{
						Type:  configurationv1alpha1.SensitiveDataSourceTypeInline,
						Value: new("certificate-pem-data"),
					},
				},
			},
		}

		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})

	t.Run("EventGatewaySchemaRegistry", func(t *testing.T) {
		obj := &configurationv1alpha1.EventGatewaySchemaRegistry{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.EventGatewaySchemaRegistrySpec{
				GatewayRef: generatedParentRef(),
				APISpec: configurationv1alpha1.EventGatewaySchemaRegistryAPISpec{
					EventGatewaySchemaRegistryConfig: &configurationv1alpha1.EventGatewaySchemaRegistryConfig{
						Type: configurationv1alpha1.EventGatewaySchemaRegistryConfigTypeSchemaRegistryConfluent,
						SchemaRegistryConfluent: &configurationv1alpha1.SchemaRegistryConfluent{
							Config: configurationv1alpha1.SchemaRegistryConfluentConfig{
								Endpoint:   "https://schema-registry.example.com",
								SchemaType: "json",
							},
							Name: "schema-registry",
						},
					},
				},
			},
		}

		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})
}
