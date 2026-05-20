package crdsvalidation

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validListenerPolicy(ns string) *konnectv1alpha1.EventGatewayListenerPolicy {
	return &konnectv1alpha1.EventGatewayListenerPolicy{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.EventGatewayListenerPolicySpec{
			EventGatewayListenerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "my-event-gateway-listener",
				},
			},
			APISpec: konnectv1alpha1.EventGatewayListenerPolicyAPISpec{
				EventGatewayListenerPolicyConfig: &konnectv1alpha1.EventGatewayListenerPolicyConfig{
					Type: konnectv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen,
					EventGatewayTLSListen: &konnectv1alpha1.EventGatewayTLSListenerPolicy{
						Name: "tls-policy",
						Config: konnectv1alpha1.EventGatewayTLSListenerPolicyConfig{
							Certificates: []konnectv1alpha1.TLSCertificate{
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
		common.TestCasesGroup[*konnectv1alpha1.EventGatewayListenerPolicy]{
			{
				Name:       "minimal valid object with inline certificates",
				TestObject: validListenerPolicy(ns.Name),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("certificates SensitiveDataSource validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.EventGatewayListenerPolicy]{
			{
				Name: "inline type with value passes",
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
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
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
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
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
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
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
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
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
						{
							Certificate: konnectv1alpha1.SensitiveDataSource{
								Type: konnectv1alpha1.SensitiveDataSourceTypeInline,
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
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
						{
							Certificate: konnectv1alpha1.SensitiveDataSource{
								Type: konnectv1alpha1.SensitiveDataSourceTypeSecretRef,
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
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
						{
							Certificate: inlineSDS("cert-pem-data"),
							Key: konnectv1alpha1.SensitiveDataSource{
								Type: konnectv1alpha1.SensitiveDataSourceTypeInline,
							},
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
			{
				Name: "key secretRef type without secretRef fails",
				TestObject: func() *konnectv1alpha1.EventGatewayListenerPolicy {
					obj := validListenerPolicy(ns.Name)
					obj.Spec.APISpec.EventGatewayTLSListen.Config.Certificates = []konnectv1alpha1.TLSCertificate{
						{
							Certificate: inlineSDS("cert-pem-data"),
							Key: konnectv1alpha1.SensitiveDataSource{
								Type: konnectv1alpha1.SensitiveDataSourceTypeSecretRef,
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
