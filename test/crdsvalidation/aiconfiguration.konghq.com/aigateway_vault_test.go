package crdsvalidation_test

import (
	"testing"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayVault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := &aiconfigurationv1alpha1.AIGatewayVault{
			Kind:       "AIGatewayVault",
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayVaultSpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayVaultAPISpec{
					AIGatewayVaultConfig: &aiconfigurationv1alpha1.AIGatewayVaultConfig{
						Type: aiconfigurationv1alpha1.AIGatewayVaultConfigTypeEnvironmentVariable,
						EnvironmentVariable: &aiconfigurationv1alpha1.EnvironmentVariableVault{
							Name: "env",
							Config: aiconfigurationv1alpha1.EnvironmentVariableVaultConfig{
								Prefix: "MY_SECRET_",
							},
						},
					},
				},
			},
		}
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj)
	})

	// The credential leaves are the only vault fields whose schema the
	// generator derives rather than copies from the OAS: each sits inside the
	// type union and, for HashiCorp, inside a second auth_method union, so
	// assert the SensitiveDataSource rule landed on the nested leaf.
	t.Run("credential data source", func(t *testing.T) {
		conjurVault := func() *aiconfigurationv1alpha1.AIGatewayVault {
			return &aiconfigurationv1alpha1.AIGatewayVault{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: aiconfigurationv1alpha1.AIGatewayVaultSpec{
					AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "aigateway-1",
						},
					},
					APISpec: aiconfigurationv1alpha1.AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &aiconfigurationv1alpha1.AIGatewayVaultConfig{
							Type: aiconfigurationv1alpha1.AIGatewayVaultConfigTypeConjur,
							Conjur: &aiconfigurationv1alpha1.ConjurVault{
								Name: "conjur",
								Config: aiconfigurationv1alpha1.ConjurVaultConfig{
									Account:     "my-org",
									Login:       "my-workload",
									EndpointURL: "https://conjur.example.com",
									APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
										Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
									},
								},
							},
						},
					},
				},
			}
		}

		const valueRequired = "value required when type=inline; secretRef required when type=secretRef"

		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayVault]{
			{
				Name: "inline without a value is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayVault {
					return conjurVault()
				}(),
				ExpectedErrorMessage: new(valueRequired),
			},
			{
				Name: "secretRef without a reference is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayVault {
					obj := conjurVault()
					obj.Spec.APISpec.Conjur.Config.APIKey = aiconfigurationv1alpha1.SensitiveDataSource{
						Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					}
					return obj
				}(),
				ExpectedErrorMessage: new(valueRequired),
			},
			{
				Name: "inline value is accepted",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayVault {
					obj := conjurVault()
					obj.Spec.APISpec.Conjur.Config.APIKey = aiconfigurationv1alpha1.SensitiveDataSource{
						Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
						Value: new("my-api-key"),
					}
					return obj
				}(),
			},
			{
				Name: "secretRef is accepted",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayVault {
					obj := conjurVault()
					obj.Spec.APISpec.Conjur.Config.APIKey = aiconfigurationv1alpha1.SensitiveDataSource{
						Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
						SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
							Name: "conjur-apikey",
							Key:  "apiKey",
						},
					}
					return obj
				}(),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
