package configuration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayVirtualClusterConsumePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("schemaRegistry reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy]{
			{
				Name: "json schema validation, which parses without a schema, referencing an EventGatewaySchemaRegistry fails",
				TestObject: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
						EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "my-event-gateway-virtual-cluster",
							},
						},
						APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
							EventGatewayVirtualClusterConsumePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
								Type: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy,
								ConsumeSchemaValidationPolicy: &configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicy{
									Name: "schema-validation-json",
									Config: &configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicyConfig{
										Type: configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicyConfigTypeJSON,
										JSON: &configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicyJSONConfig{
											SchemaRegistry: &configurationv1alpha1.EventGatewaySchemaRegistryRefWithNamespacedRef{
												Name: "my-schema-registry",
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("json.schemaRegistry can not be set with the type json, which parses without a schema"),
			},
			{
				Name: "confluentSchemaRegistry schema validation referencing an EventGatewaySchemaRegistry by name",
				TestObject: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
						EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "my-event-gateway-virtual-cluster",
							},
						},
						APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
							EventGatewayVirtualClusterConsumePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
								Type: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy,
								ConsumeSchemaValidationPolicy: &configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicy{
									Name: "schema-validation-confluent",
									Config: &configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicyConfig{
										Type: configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicyConfigTypeSchemaRegistry,
										SchemaRegistry: &configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicySchemaRegistryConfig{
											SchemaRegistry: &configurationv1alpha1.EventGatewaySchemaRegistryRefWithNamespacedRef{
												Name: "my-schema-registry",
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

	t.Run("schemaRegistry reference forms", func(t *testing.T) {
		policy := func(schemaRegistry string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
			obj := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{ObjectMeta: common.CommonObjectMeta(ns.Name)}
			obj.Spec.EventGatewayVirtualClusterRef = commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "my-event-gateway-virtual-cluster"},
			}
			require.NoError(t, yaml.UnmarshalStrict([]byte(`
type: schemaValidation
schemaValidation:
  name: validate
  config:
    type: confluentSchemaRegistry
    confluentSchemaRegistry:
      valueValidationAction: skip
      schemaRegistry: `+schemaRegistry), &obj.Spec.APISpec))
			return obj
		}
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy]{
			{
				Name:       "the name of the object, set directly, passes",
				TestObject: policy(`{name: orders}`),
			},
			{
				Name:       "the name, kind and namespace of the object, set directly, pass",
				TestObject: policy(`{kind: EventGatewaySchemaRegistry, name: orders, namespace: ` + ns.Name + `}`),
			},
			{
				Name:       "type namespacedRef passes",
				TestObject: policy(`{type: namespacedRef, namespacedRef: {name: orders}}`),
			},
			{
				Name:                 "neither name nor namespacedRef fails",
				TestObject:           policy(`{}`),
				ExpectedErrorMessage: new("exactly one of name and namespacedRef must be set"),
			},
			{
				Name:                 "both name and namespacedRef fail",
				TestObject:           policy(`{type: namespacedRef, name: orders, namespacedRef: {name: orders}}`),
				ExpectedErrorMessage: new("exactly one of name and namespacedRef must be set"),
			},
			{
				Name:                 "type namespacedRef without namespacedRef fails",
				TestObject:           policy(`{type: namespacedRef, name: orders}`),
				ExpectedErrorMessage: new("type namespacedRef must be set with namespacedRef, and only with it"),
			},
			{
				Name:                 "namespacedRef without type fails",
				TestObject:           policy(`{namespacedRef: {name: orders}}`),
				ExpectedErrorMessage: new("type namespacedRef must be set with namespacedRef, and only with it"),
			},
			{
				Name:                 "kind next to namespacedRef fails",
				TestObject:           policy(`{type: namespacedRef, kind: EventGatewaySchemaRegistry, namespacedRef: {name: orders}}`),
				ExpectedErrorMessage: new("kind and namespace must not be set next to namespacedRef: remove kind, and set the namespace in namespacedRef"),
			},
			{
				Name:                 "another type fails",
				TestObject:           policy(`{type: name, name: orders}`),
				ExpectedErrorMessage: new("Unsupported value: \"name\""),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("parentPolicyRef of maskFields and transcode", func(t *testing.T) {
		policy := func(apiSpec string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
			obj := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{ObjectMeta: common.CommonObjectMeta(ns.Name)}
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
    outputFormat: json
    json:
      failureMode: error
      schemaSource:
        type: reference
        reference:
          subject: orders-value
          version: latest
          schemaRefDestination: {type: none, none: {}}
          schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}
`
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy]{
			{
				Name:       "a transcode policy with a parent passes",
				TestObject: policy("parentPolicyRef: {name: parent}" + transcode),
			},
			{
				Name:                 "a transcode policy without a parent fails",
				TestObject:           policy(transcode),
				ExpectedErrorMessage: new("parentPolicyRef is required for decryptFields, maskFields and transcode"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
