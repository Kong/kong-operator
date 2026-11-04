package crdsvalidation_test

import (
	"testing"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayAuthStrategy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
			Kind:       "AIGatewayAuthStrategy",
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
					AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
						Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeKeyAuth,
						KeyAuth: &aiconfigurationv1alpha1.AIGatewayAuthStrategyKeyAuth{
							Name:        "provider1",
							DisplayName: "Test Auth Strategy",
						},
					},
				},
			},
		}
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj)
	})

	t.Run("OIDC Redis credentials", func(t *testing.T) {
		const valueRequired = "value required when type=inline; secretRef required when type=secretRef"

		redisOIDC := func() *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
			return &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
				Kind:       "AIGatewayAuthStrategy",
				APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
					AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "aigateway-1",
						},
					},
					APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
						AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
							Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeOpenIDConnect,
							OpenIDConnect: &aiconfigurationv1alpha1.AIGatewayAuthStrategyOpenIDConnect{
								Name:        "oidc-with-redis",
								DisplayName: "OIDC with Redis",
								Config: aiconfigurationv1alpha1.AIGWOpenIDConnectGeneratedConfig{
									Issuer:          "https://oidc.example.com/.well-known/openid-configuration",
									CacheTokensSalt: "oidc-cache-salt",
									Redis: aiconfigurationv1alpha1.AIGWOpenIDConnectGeneratedConfigRedis{
										Password: aiconfigurationv1alpha1.SensitiveDataSource{
											Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
											SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
												Name: "redis-password",
												Key:  "password",
											},
										},
										SentinelPassword: aiconfigurationv1alpha1.SensitiveDataSource{
											Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
											SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
												Name: "redis-password",
												Key:  "sentinelPassword",
											},
										},
									},
								},
							},
						},
					},
				},
			}
		}

		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayAuthStrategy]{
			{
				Name:       "secretRef is accepted",
				TestObject: redisOIDC(),
			},
			{
				Name: "inline without a value is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
					obj := redisOIDC()
					obj.Spec.APISpec.OpenIDConnect.Config.Redis.Password = aiconfigurationv1alpha1.SensitiveDataSource{
						Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					}
					return obj
				}(),
				ExpectedErrorMessage: new(valueRequired),
			},
			{
				// Both shapes at once would ship the inline value even when type
				// is secretRef, so a rotation that forgets to clear the old value
				// keeps sending the old credential.
				Name: "value and secretRef together are rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
					obj := redisOIDC()
					obj.Spec.APISpec.OpenIDConnect.Config.Redis.Password.Value = new("stale-password")
					return obj
				}(),
				ExpectedErrorMessage: new("only one of value and secretRef can be set"),
			},
			{
				Name: "inline value is accepted",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
					obj := redisOIDC()
					obj.Spec.APISpec.OpenIDConnect.Config.Redis.Password = aiconfigurationv1alpha1.SensitiveDataSource{
						Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
						Value: new("my-password"),
					}
					return obj
				}(),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
