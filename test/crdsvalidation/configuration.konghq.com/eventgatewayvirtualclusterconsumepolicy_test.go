package configuration_test

import (
	"testing"

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
				Name: "json schema validation referencing an EventGatewaySchemaRegistry by name",
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
											SchemaRegistry: &configurationv1alpha1.EventGatewaySchemaRegistryRef{
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
											SchemaRegistry: &configurationv1alpha1.EventGatewaySchemaRegistryRef{
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
}
