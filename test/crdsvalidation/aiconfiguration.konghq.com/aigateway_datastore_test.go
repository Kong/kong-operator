package crdsvalidation_test

import (
	"testing"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayDatastore(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	redisCE := func(nsName string) *aiconfigurationv1alpha1.AIGatewayDatastore {
		return &aiconfigurationv1alpha1.AIGatewayDatastore{
			Kind:       "AIGatewayDatastore",
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			ObjectMeta: common.CommonObjectMeta(nsName),
			Spec: aiconfigurationv1alpha1.AIGatewayDatastoreSpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayDatastoreAPISpec{
					AIGatewayDatastoreConfig: &aiconfigurationv1alpha1.AIGatewayDatastoreConfig{
						Type: aiconfigurationv1alpha1.AIGatewayDatastoreConfigTypeRedisCE,
						RedisCE: &aiconfigurationv1alpha1.RedisCEDatastore{
							Name:        "shared-redis",
							DisplayName: "Shared Redis",
							Config: aiconfigurationv1alpha1.RedisCEDatastoreConfig{
								Host: "redis.example.com",
								Port: 6379,
								Password: aiconfigurationv1alpha1.SensitiveDataSource{
									Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
									SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
										Name: "redis-password",
										Key:  "password",
									},
								},
							},
						},
					},
				},
			},
		}
	}

	vectorDB := func(nsName string) *aiconfigurationv1alpha1.AIGatewayDatastore {
		return &aiconfigurationv1alpha1.AIGatewayDatastore{
			Kind:       "AIGatewayDatastore",
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			ObjectMeta: common.CommonObjectMeta(nsName),
			Spec: aiconfigurationv1alpha1.AIGatewayDatastoreSpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayDatastoreAPISpec{
					AIGatewayDatastoreConfig: &aiconfigurationv1alpha1.AIGatewayDatastoreConfig{
						Type: aiconfigurationv1alpha1.AIGatewayDatastoreConfigTypeVectorDB,
						VectorDB: &aiconfigurationv1alpha1.VectorDBDatastore{
							Name:        "shared-pgvector",
							DisplayName: "Shared pgvector",
							Config: aiconfigurationv1alpha1.VectorDBDatastoreConfig{
								Host:     "pgvector.example.com",
								Database: "kong-pgvector",
							},
						},
					},
				},
			},
		}
	}

	t.Run("AI Gateway ref", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, redisCE(ns.Name))
	})

	t.Run("credential data source", func(t *testing.T) {
		const valueRequired = "value required when type=inline; secretRef required when type=secretRef"

		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayDatastore]{
			{
				Name: "secretRef is accepted",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayDatastore {
					return redisCE(ns.Name)
				}(),
			},
			{
				Name: "inline without a value is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayDatastore {
					obj := redisCE(ns.Name)
					obj.Spec.APISpec.RedisCE.Config.Password = aiconfigurationv1alpha1.SensitiveDataSource{
						Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					}
					return obj
				}(),
				ExpectedErrorMessage: new(valueRequired),
			},
			{
				Name: "inline value is accepted",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayDatastore {
					obj := redisCE(ns.Name)
					obj.Spec.APISpec.RedisCE.Config.Password = aiconfigurationv1alpha1.SensitiveDataSource{
						Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
						Value: new("my-password"),
					}
					return obj
				}(),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("immutable fields", func(t *testing.T) {
		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayDatastore]{
			{
				Name:       "redis-ce name is immutable",
				TestObject: redisCE(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayDatastore) {
					obj.Spec.APISpec.RedisCE.Name = "renamed-redis"
				},
				ExpectedUpdateErrorMessage: new("name is immutable"),
			},
			{
				Name:       "vectordb name is immutable",
				TestObject: vectorDB(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayDatastore) {
					obj.Spec.APISpec.VectorDB.Name = "renamed-pgvector"
				},
				ExpectedUpdateErrorMessage: new("name is immutable"),
			},
			{
				Name:       "type cannot change",
				TestObject: redisCE(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayDatastore) {
					obj.Spec.APISpec.AIGatewayDatastoreConfig = vectorDB(ns.Name).Spec.APISpec.AIGatewayDatastoreConfig
				},
				ExpectedUpdateErrorMessage: new("type is immutable"),
			},
			{
				Name:       "display name and description can be updated",
				TestObject: redisCE(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayDatastore) {
					obj.Spec.APISpec.RedisCE.DisplayName = "Renamed"
					obj.Spec.APISpec.RedisCE.Description = "Updated description"
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
