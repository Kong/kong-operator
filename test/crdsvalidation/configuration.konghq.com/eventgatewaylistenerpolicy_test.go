package configuration_test

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validListenerPolicy(ns string) *configurationv1alpha1.EventGatewayListenerPolicy {
	return &configurationv1alpha1.EventGatewayListenerPolicy{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
			EventGatewayListenerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "my-event-gateway-listener",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayListenerPolicyAPISpec{
				EventGatewayListenerPolicyConfig: &configurationv1alpha1.EventGatewayListenerPolicyConfig{
					Type: configurationv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen,
					EventGatewayTLSListen: &configurationv1alpha1.EventGatewayTLSListenerPolicy{
						Name: "tls-policy",
						Config: configurationv1alpha1.EventGatewayTLSListenerPolicyConfig{
							Certificates: []configurationv1alpha1.TLSCertificate{
								{
									Certificate: inlineSDS("cert-pem-data"),
									Key:         inlineSDS("key-pem-data"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestEventGatewayListenerPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("valid object", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayListenerPolicy]{
			{
				Name:       "minimal valid object with inline certificates",
				TestObject: validListenerPolicy(ns.Name),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("certificates SensitiveDataSource validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayListenerPolicy]{
			{
				Name: "inline type with value passes",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: inlineSDS("cert-pem-data"),
							Key:         inlineSDS("key-pem-data"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "secretRef type with secretRef passes",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: secretRefSDS("my-tls-secret"),
							Key:         secretRefSDS("my-tls-secret"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "mixed inline cert and secretRef key passes",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: inlineSDS("cert-pem-data"),
							Key:         secretRefSDS("my-tls-secret"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "multiple certificates each independently valid passes",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: inlineSDS("cert-pem-data-1"),
							Key:         inlineSDS("key-pem-data-1"),
						},
						{
							Certificate: secretRefSDS("my-tls-secret-2"),
							Key:         secretRefSDS("my-tls-secret-2"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "inline type without value fails",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: configurationv1alpha1.SensitiveDataSource{
								Type: configurationv1alpha1.SensitiveDataSourceTypeInline,
							},
							Key: inlineSDS("key-pem-data"),
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
			{
				Name: "secretRef type without secretRef fails",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: configurationv1alpha1.SensitiveDataSource{
								Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
							},
							Key: inlineSDS("key-pem-data"),
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
			{
				Name: "key inline type without value fails",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: inlineSDS("cert-pem-data"),
							Key: configurationv1alpha1.SensitiveDataSource{
								Type: configurationv1alpha1.SensitiveDataSourceTypeInline,
							},
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
			{
				Name: "key secretRef type without secretRef fails",
				TestObject: func() *configurationv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []configurationv1alpha1.TLSCertificate{
						{
							Certificate: inlineSDS("cert-pem-data"),
							Key: configurationv1alpha1.SensitiveDataSource{
								Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
							},
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
