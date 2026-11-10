package configuration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayVirtualClusterProducePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("valid object", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name: "minimal valid object",
				TestObject: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
						EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "my-event-gateway-virtual-cluster",
							},
						},
						APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
							EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
								Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate,
								ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{
									Config: configurationv1alpha1.EventGatewayModifyHeadersPolicyCreateConfig{
										Actions: []configurationv1alpha1.EventGatewayModifyHeaderAction{
											{
												Op: configurationv1alpha1.EventGatewayModifyHeaderActionTypeSet,
												Set: &configurationv1alpha1.EventGatewayModifyHeaderSetAction{
													Key:   "x-produced-header",
													Value: "set-by-test",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("encrypt policy static key reference", func(t *testing.T) {
		withKey := func(key *configurationv1alpha1.EncryptionKeyStaticReference) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
			return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
					EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
						Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "my-event-gateway-virtual-cluster"},
					},
					APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
						EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
							Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeEncryptPolicy,
							EncryptPolicy: &configurationv1alpha1.EventGatewayEncryptPolicy{
								Name: "encrypt",
								Config: configurationv1alpha1.EventGatewayEncryptConfig{
									FailureMode:  "error",
									PartOfRecord: []configurationv1alpha1.EncryptionRecordPart{"value"},
									EncryptionKey: &configurationv1alpha1.EventGatewayEncryptConfigEncryptionKey{
										Type:   configurationv1alpha1.EventGatewayEncryptConfigEncryptionKeyTypeStatic,
										Static: &configurationv1alpha1.EncryptionKeyStatic{Key: key},
									},
								},
							},
						},
					},
				},
			}
		}
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name:       "Konnect static key ID passes",
				TestObject: withKey(&configurationv1alpha1.EncryptionKeyStaticReference{ID: new("7f3c2a6e-1b9d-4c8e-9f0a-2d4b6c8e0a1f")}),
			},
			{
				Name:       "Konnect static key name passes",
				TestObject: withKey(&configurationv1alpha1.EncryptionKeyStaticReference{Name: new("encryption-key")}),
			},
			{
				Name: "namespacedRef to an EventGatewayStaticKey passes",
				TestObject: withKey(&configurationv1alpha1.EncryptionKeyStaticReference{
					NamespacedRef: &configurationv1alpha1.EventGatewayStaticKeyRef{Name: "static-key"},
				}),
			},
			{
				Name: "setting both a Konnect ID and a namespacedRef fails",
				TestObject: withKey(&configurationv1alpha1.EncryptionKeyStaticReference{
					ID:            new("7f3c2a6e-1b9d-4c8e-9f0a-2d4b6c8e0a1f"),
					NamespacedRef: &configurationv1alpha1.EventGatewayStaticKeyRef{Name: "static-key"},
				}),
				ExpectedErrorMessage: new("must have at most 1 item"),
			},
			{
				Name: "namespacedRef with an unsupported kind fails",
				TestObject: withKey(&configurationv1alpha1.EncryptionKeyStaticReference{
					NamespacedRef: &configurationv1alpha1.EventGatewayStaticKeyRef{Kind: "EventGatewaySchemaRegistry", Name: "static-key"},
				}),
				ExpectedErrorMessage: new(`supported values: "EventGatewayStaticKey"`),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("schema validation schema registry reference", func(t *testing.T) {
		withRegistry := func(registry *configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
			return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
					EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
						Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "my-event-gateway-virtual-cluster"},
					},
					APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
						EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
							Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy,
							ProduceSchemaValidationPolicy: &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicy{
								Name: "validate",
								Config: &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyConfig{
									Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyConfigTypeJSON,
									JSON: &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfig{
										SchemaRegistry:        registry,
										ValueValidationAction: "reject",
									},
								},
							},
						},
					},
				},
			}
		}
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name: "a namespacedRef with the type json, which parses without a schema, fails",
				TestObject: withRegistry(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry{
					Type:          configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistryTypeNamespacedRef,
					NamespacedRef: &configurationv1alpha1.EventGatewaySchemaRegistryRef{Name: "orders-registry"},
				}),
				ExpectedErrorMessage: new("json.schemaRegistry can not be set with the type json, which parses without a schema"),
			},
			{
				Name: "namespacedRef with the type confluentSchemaRegistry passes",
				TestObject: func() *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
					obj := withRegistry(nil)
					obj.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig.ProduceSchemaValidationPolicy.Config = &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyConfig{
						Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyConfigTypeSchemaRegistry,
						SchemaRegistry: &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfig{
							SchemaRegistry: &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry{
								Type:          configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistryTypeNamespacedRef,
								NamespacedRef: &configurationv1alpha1.EventGatewaySchemaRegistryRef{Name: "orders-registry"},
							},
							ValueValidationAction: "reject",
						},
					}
					return obj
				}(),
			},
			{
				Name: "a Konnect schema registry name with the type json fails",
				TestObject: withRegistry(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry{
					Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistryTypeName,
					Name: &configurationv1alpha1.SchemaRegistryReferenceByName{Name: "orders-registry"},
				}),
				ExpectedErrorMessage: new("json.schemaRegistry can not be set with the type json, which parses without a schema"),
			},
			{
				Name:       "the type json without a schema registry passes",
				TestObject: withRegistry(nil),
			},
			{
				Name:       "adding a schema registry to the type json on update fails",
				TestObject: withRegistry(nil),
				Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
					obj.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig.ProduceSchemaValidationPolicy.Config.JSON.SchemaRegistry =
						&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry{
							Type:          configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistryTypeNamespacedRef,
							NamespacedRef: &configurationv1alpha1.EventGatewaySchemaRegistryRef{Name: "orders-registry"},
						}
				},
				ExpectedUpdateErrorMessage: new("json.schemaRegistry can not be set with the type json, which parses without a schema"),
			},
			{
				Name: "namespacedRef with an unsupported kind fails",
				TestObject: withRegistry(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry{
					Type:          configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistryTypeNamespacedRef,
					NamespacedRef: &configurationv1alpha1.EventGatewaySchemaRegistryRef{Kind: "EventGatewayStaticKey", Name: "orders-registry"},
				}),
				ExpectedErrorMessage: new(`supported values: "EventGatewaySchemaRegistry"`),
			},
			{
				Name: "an unknown reference type fails",
				TestObject: withRegistry(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry{
					Type:          "objectRef",
					NamespacedRef: &configurationv1alpha1.EventGatewaySchemaRegistryRef{Name: "orders-registry"},
				}),
				ExpectedErrorMessage: new(`supported values: "id", "name", "namespacedRef"`),
			},
		}.RunWithConfig(t, cfg, scheme)

		withConfluent := func(registry *configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
			obj := withRegistry(nil)
			obj.Spec.APISpec.ProduceSchemaValidationPolicy.Config = &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyConfig{
				Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyConfigTypeSchemaRegistry,
				SchemaRegistry: &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfig{
					SchemaRegistry:        registry,
					ValueValidationAction: "reject",
				},
			}
			return obj
		}
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name: "type namespacedRef without namespacedRef fails",
				TestObject: withConfluent(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry{
					Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistryTypeNamespacedRef,
				}),
				ExpectedErrorMessage: new("namespacedRef must be set only when type is namespacedRef"),
			},
			{
				Name: "type name with name passes",
				TestObject: withConfluent(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry{
					Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistryTypeName,
					Name: &configurationv1alpha1.SchemaRegistryReferenceByName{Name: "orders-registry"},
				}),
			},
			{
				Name: "type id with an id passes, and so does an unrelated update",
				TestObject: withConfluent(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry{
					Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistryTypeID,
					ID:   &configurationv1alpha1.SchemaRegistryReferenceByID{ID: "6f1d3c9a-2b7e-4c5d-8a1f-9e3b7c2d4f60"},
				}),
				Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
					obj.Spec.APISpec.ProduceSchemaValidationPolicy.Config.SchemaRegistry.ValueValidationAction = "mark"
				},
			},
			{
				// The id isn't a required property, so that a stored type id
				// reference, which the released CRD stored without its id,
				// can still be updated: the rule of the union requires it,
				// for a new or changed reference.
				Name: "type id without its id fails",
				TestObject: withConfluent(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry{
					Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistryTypeID,
					ID:   &configurationv1alpha1.SchemaRegistryReferenceByID{},
				}),
				ExpectedErrorMessage: new("id must be set only when type is id"),
			},
			{
				Name: "changing the reference to type id without its id fails",
				TestObject: withConfluent(&configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry{
					Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistryTypeName,
					Name: &configurationv1alpha1.SchemaRegistryReferenceByName{Name: "orders-registry"},
				}),
				Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
					obj.Spec.APISpec.ProduceSchemaValidationPolicy.Config.SchemaRegistry.SchemaRegistry = &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistry{
						Type: configurationv1alpha1.EventGatewayProduceSchemaValidationPolicySchemaRegistryConfigSchemaRegistryTypeID,
						ID:   &configurationv1alpha1.SchemaRegistryReferenceByID{},
					}
				},
				ExpectedUpdateErrorMessage: new("id must be set only when type is id"),
			},
		}.RunWithConfig(t, cfg, scheme)

		// A json schema validation with a schema registry, refused since, is
		// only kept by stored objects: they can't be created in envtest, but
		// a json schema validation gaining one on update is still refused.
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name:       "a json schema validation gaining a schema registry on update fails",
				TestObject: withRegistry(nil),
				Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
					obj.Spec.APISpec.ProduceSchemaValidationPolicy.Config.JSON.SchemaRegistry = &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry{
						Type:          configurationv1alpha1.EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistryTypeNamespacedRef,
						NamespacedRef: &configurationv1alpha1.EventGatewaySchemaRegistryRef{Name: "orders-registry"},
					}
				},
				ExpectedUpdateErrorMessage: new("json.schemaRegistry can not be set with the type json, which parses without a schema"),
			},
		}.RunWithConfig(t, cfg, scheme)

		// The typed client only encodes the member of the type: a stray member,
		// which kubectl would send, is sent as unstructured.
		t.Run("type name with a stray namespacedRef fails", func(t *testing.T) {
			cl, err := client.New(cfg, client.Options{Scheme: scheme})
			require.NoError(t, err)
			obj := &unstructured.Unstructured{}
			require.NoError(t, yaml.Unmarshal([]byte(`
apiVersion: configuration.konghq.com/v1alpha1
kind: EventGatewayVirtualClusterProducePolicy
metadata: {generateName: stray-, namespace: `+ns.Name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: my-event-gateway-virtual-cluster}}
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: validate
      config:
        type: confluentSchemaRegistry
        confluentSchemaRegistry:
          valueValidationAction: reject
          schemaRegistry: {type: name, name: {name: orders-registry}, namespacedRef: {name: orders-registry}}
`), &obj.Object))
			require.ErrorContains(t, cl.Create(t.Context(), obj), "namespacedRef must be set only when type is namespacedRef")
		})
	})

	t.Run("parentPolicyRef of maskFields and transcode", func(t *testing.T) {
		policy := func(apiSpec string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
			obj := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{ObjectMeta: common.CommonObjectMeta(ns.Name)}
			obj.Spec.EventGatewayVirtualClusterRef = commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "my-event-gateway-virtual-cluster"},
			}
			require.NoError(t, yaml.UnmarshalStrict([]byte(apiSpec), &obj.Spec.APISpec))
			return obj
		}
		const transcode = `
type: transcode
transcode:
  name: transcode
  config:
    outputFormat: avro
    avro:
      failureMode: reject
      schemaSource:
        type: reference
        reference:
          subject: orders-value
          version: latest
          schemaRefDestination: {type: none, none: {}}
          schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}
`
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name:       "a transcode policy with a parent passes",
				TestObject: policy("parentPolicyRef: {name: parent}" + transcode),
			},
			{
				Name:                 "a transcode policy without a parent fails",
				TestObject:           policy(transcode),
				ExpectedErrorMessage: new("parentPolicyRef is required for encryptFields, maskFields and transcode"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
