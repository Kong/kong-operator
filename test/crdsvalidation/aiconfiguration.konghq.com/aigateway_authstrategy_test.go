package crdsvalidation_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
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

		t.Run("legacy string passwords survive a CRD upgrade", func(t *testing.T) {
			cl, err := client.New(cfg, client.Options{Scheme: scheme})
			require.NoError(t, err)

			const crdName = "aigatewayauthstrategies.aiconfiguration.konghq.com"
			crd := &apiextensionsv1.CustomResourceDefinition{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: crdName}, crd))
			currentSpec := crd.Spec.DeepCopy()
			for i := range crd.Spec.Versions {
				schema := crd.Spec.Versions[i].Schema.OpenAPIV3Schema
				config := schema.Properties["spec"].Properties["apiSpec"].Properties["openid-connect"].Properties["config"]
				for _, blockName := range []string{"redis", "clusterCacheRedis"} {
					block := config.Properties[blockName]
					for _, fieldName := range []string{"password", "sentinelPassword"} {
						field := block.Properties[fieldName]
						block.Properties[fieldName] = apiextensionsv1.JSONSchemaProps{
							Description: field.Description,
							Type:        "string",
							MaxLength:   new(int64(253)),
						}
					}
					config.Properties[blockName] = block
				}
			}
			require.NoError(t, cl.Update(t.Context(), crd))

			newLegacy := func() *unstructured.Unstructured {
				data, err := json.Marshal(redisOIDC())
				require.NoError(t, err)
				obj := &unstructured.Unstructured{}
				require.NoError(t, json.Unmarshal(data, &obj.Object))
				for _, blockName := range []string{"redis", "clusterCacheRedis"} {
					for _, fieldName := range []string{"password", "sentinelPassword"} {
						require.NoError(t, unstructured.SetNestedField(
							obj.Object,
							"legacy-password",
							"spec", "apiSpec", "openid-connect", "config", blockName, fieldName,
						))
					}
				}
				return obj
			}
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.NoError(ct, cl.Create(t.Context(), newLegacy(), client.DryRunAll))
			}, consts.WaitTime, consts.TickTime)
			legacy := newLegacy()
			require.NoError(t, cl.Create(t.Context(), legacy))

			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: crdName}, crd))
			crd.Spec = *currentSpec
			require.NoError(t, cl.Update(t.Context(), crd))
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				err := cl.Create(t.Context(), newLegacy(), client.DryRunAll)
				assert.True(ct, apierrors.IsInvalid(err), "%v", err)
			}, consts.WaitTime, consts.TickTime)

			legacy.SetLabels(map[string]string{"updated": "true"})
			err = cl.Update(t.Context(), legacy)
			require.True(t, apierrors.IsInvalid(err), "%v", err)
			require.ErrorContains(t, err, "password")
			require.NoError(t, unstructured.SetNestedSlice(legacy.Object, []any{}, "status", "conditions"))
			err = cl.Status().Update(t.Context(), legacy)
			require.True(t, apierrors.IsInvalid(err), "%v", err)
			require.ErrorContains(t, err, "password")

			stored := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(legacy), stored))
			config := stored.Spec.APISpec.OpenIDConnect.Config
			for _, password := range []aiconfigurationv1alpha1.SensitiveDataSource{
				config.ClusterCacheRedis.Password,
				config.ClusterCacheRedis.SentinelPassword,
				config.Redis.Password,
				config.Redis.SentinelPassword,
			} {
				require.Equal(t, aiconfigurationv1alpha1.SensitiveDataSourceTypeInline, password.Type)
				require.Equal(t, "legacy-password", password.GetValue())
			}

			stored.SetLabels(map[string]string{"migrated": "true"})
			require.NoError(t, cl.Update(t.Context(), stored))
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(legacy), legacy))
			for _, blockName := range []string{"redis", "clusterCacheRedis"} {
				for _, fieldName := range []string{"password", "sentinelPassword"} {
					value, found, err := unstructured.NestedMap(
						legacy.Object,
						"spec", "apiSpec", "openid-connect", "config", blockName, fieldName,
					)
					require.NoError(t, err)
					require.True(t, found)
					require.Equal(t, "inline", value["type"])
					require.Equal(t, "legacy-password", value["value"])
				}
			}

			legacy.SetLabels(map[string]string{"updated-after-migration": "true"})
			require.NoError(t, cl.Update(t.Context(), legacy))
			require.NoError(t, unstructured.SetNestedSlice(legacy.Object, []any{}, "status", "conditions"))
			require.NoError(t, cl.Status().Update(t.Context(), legacy))
		})
	})
}
