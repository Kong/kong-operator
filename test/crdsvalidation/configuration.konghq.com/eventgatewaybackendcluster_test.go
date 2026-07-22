package configuration_test

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validBackendCluster(ns string) *configurationv1alpha1.EventGatewayBackendCluster {
	return &configurationv1alpha1.EventGatewayBackendCluster{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "my-event-gateway",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayBackendClusterAPISpec{
				Name: "backend-cluster-1",
				Authentication: &configurationv1alpha1.EventGatewayBackendClusterAuthentication{
					Type: configurationv1alpha1.EventGatewayBackendClusterAuthenticationTypeAnonymous,
				},
				BootstrapServers: []string{"kafka:9092"},
				TLS: configurationv1alpha1.BackendClusterTLS{
					Enabled: "Disabled",
				},
			},
		},
	}
}

func inlineSDS(value string) configurationv1alpha1.SensitiveDataSource {
	return configurationv1alpha1.SensitiveDataSource{
		Type:  configurationv1alpha1.SensitiveDataSourceTypeInline,
		Value: new(value),
	}
}

func secretRefSDS(name, key string) configurationv1alpha1.SensitiveDataSource {
	return configurationv1alpha1.SensitiveDataSource{
		Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{
			Name: name,
			Key:  key,
		},
	}
}

func TestEventGatewayBackendCluster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("valid object", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayBackendCluster]{
			{
				Name:       "minimal valid object",
				TestObject: validBackendCluster(ns.Name),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("clientIdentity SensitiveDataSource validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayBackendCluster]{
			{
				Name: "inline type with value passes",
				TestObject: func() *configurationv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = configurationv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: configurationv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: inlineSDS("cert-pem-data"),
							Key:         inlineSDS("key-pem-data"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "secretRef type with secretRef passes",
				TestObject: func() *configurationv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = configurationv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: configurationv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: secretRefSDS("my-cert-secret", "tls.crt"),
							Key:         secretRefSDS("my-key-secret", "tls.key"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "mixed inline cert and secretRef key passes",
				TestObject: func() *configurationv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = configurationv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: configurationv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: inlineSDS("cert-data"),
							Key:         secretRefSDS("my-tls-secret", "tls.key"),
						},
					}
					return obj
				}(),
			},
			{
				Name: "inline type without value fails",
				TestObject: func() *configurationv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = configurationv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: configurationv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: configurationv1alpha1.SensitiveDataSource{
								Type: configurationv1alpha1.SensitiveDataSourceTypeInline,
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
				TestObject: func() *configurationv1alpha1.EventGatewayBackendCluster {
					obj := validBackendCluster(ns.Name)
					obj.Spec.APISpec.TLS = configurationv1alpha1.BackendClusterTLS{
						Enabled: "Enabled",
						ClientIdentity: configurationv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: configurationv1alpha1.SensitiveDataSource{
								Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
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
