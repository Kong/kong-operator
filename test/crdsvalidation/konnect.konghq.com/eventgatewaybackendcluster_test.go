package crdsvalidation

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validBackendCluster(ns string) *konnectv1alpha1.EventGatewayBackendCluster {
	return &konnectv1alpha1.EventGatewayBackendCluster{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "my-event-gateway",
				},
			},
			APISpec: konnectv1alpha1.EventGatewayBackendClusterAPISpec{
				Name: "backend-cluster-1",
				Authentication: &konnectv1alpha1.EventGatewayBackendClusterAuthentication{
					Type: konnectv1alpha1.EventGatewayBackendClusterAuthenticationTypeAnonymous,
				},
				BootstrapServers: []string{"kafka:9092"},
				TLS: konnectv1alpha1.BackendClusterTLS{
					Enabled: "Disabled",
				},
			},
		},
	}
}

func inlineSDS(value string) konnectv1alpha1.SensitiveDataSource {
	return konnectv1alpha1.SensitiveDataSource{
		Type:  konnectv1alpha1.SensitiveDataSourceTypeInline,
		Value: new(value),
	}
}

func secretRefSDS(name string) konnectv1alpha1.SensitiveDataSource {
	return konnectv1alpha1.SensitiveDataSource{
		Type: konnectv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &commonv1alpha1.NamespacedRef{
			Name: name,
		},
	}
}

func TestEventGatewayBackendCluster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("valid object", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.EventGatewayBackendCluster]{
			{
				Name:       "minimal valid object",
				TestObject: validBackendCluster(ns.Name),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("clientIdentity SensitiveDataSource validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.EventGatewayBackendCluster]{
			{
				Name: "inline type with value passes",
				TestObject: func() *konnectv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = konnectv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: konnectv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: inlineSDS("cert-pem-data"),
							Key:         inlineSDS("key-pem-data"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "secretRef type with secretRef passes",
				TestObject: func() *konnectv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = konnectv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: konnectv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: secretRefSDS("my-cert-secret"),
							Key:         secretRefSDS("my-key-secret"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "mixed inline cert and secretRef key passes",
				TestObject: func() *konnectv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = konnectv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: konnectv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: inlineSDS("cert-data"),
							Key:         secretRefSDS("my-tls-secret"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "inline type without value fails",
				TestObject: func() *konnectv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = konnectv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: konnectv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: konnectv1alpha1.SensitiveDataSource{
								Type: konnectv1alpha1.SensitiveDataSourceTypeInline,
							},
							Key: inlineSDS("key-data"),
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
			{
				Name: "secretRef type without secretRef fails",
				TestObject: func() *konnectv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = konnectv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: konnectv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: konnectv1alpha1.SensitiveDataSource{
								Type: konnectv1alpha1.SensitiveDataSourceTypeSecretRef,
							},
							Key: inlineSDS("key-data"),
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
