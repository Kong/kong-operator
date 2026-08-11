package crdsvalidation_test

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func generatedParentRef(name string) commonv1alpha1.ObjectRef {
	return commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: name,
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
				GatewayRef: generatedParentRef("my-event-gateway"),
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
				GatewayRef: generatedParentRef("my-event-gateway"),
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

	t.Run("PortalCustomDomain", func(t *testing.T) {
		obj := &konnectv1alpha1.PortalCustomDomain{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.PortalCustomDomainSpec{
				PortalRef: generatedParentRef("my-portal"),
				APISpec: konnectv1alpha1.PortalCustomDomainAPISpec{
					Enabled:  "Enabled",
					Hostname: "portal.example.com",
					SSL: &konnectv1alpha1.PortalCustomDomainSSL{
						Type: konnectv1alpha1.PortalCustomDomainSSLTypeStandard,
						Standard: &konnectv1alpha1.CreatePortalCustomDomainSSLStandard{
							DomainVerificationMethod: "http",
						},
					},
				},
			},
		}

		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})

	t.Run("PortalCustomization", func(t *testing.T) {
		obj := &konnectv1alpha1.PortalCustomization{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.PortalCustomizationSpec{
				PortalRef: generatedParentRef("my-portal"),
			},
		}

		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})

	t.Run("PortalEmailConfig", func(t *testing.T) {
		obj := &konnectv1alpha1.PortalEmailConfig{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.PortalEmailConfigSpec{
				PortalRef: generatedParentRef("my-portal"),
			},
		}

		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})

	t.Run("PortalIPAllowList", func(t *testing.T) {
		obj := &konnectv1alpha1.PortalIPAllowList{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.PortalIPAllowListSpec{
				PortalRef: generatedParentRef("my-portal"),
				APISpec: konnectv1alpha1.PortalIPAllowListAPISpec{
					AllowedIps: []string{"127.0.0.1"},
				},
			},
		}

		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})

	t.Run("PortalTeam", func(t *testing.T) {
		obj := &konnectv1alpha1.PortalTeam{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.PortalTeamSpec{
				PortalRef: generatedParentRef("my-portal"),
				APISpec: konnectv1alpha1.PortalTeamAPISpec{
					Name: "platform-team",
				},
			},
		}

		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})
}
