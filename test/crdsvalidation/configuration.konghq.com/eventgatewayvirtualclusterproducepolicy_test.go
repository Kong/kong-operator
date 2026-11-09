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
		withKey := func(key configurationv1alpha1.EventGatewayStaticKeyRef) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
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
				Name:       "a reference to an EventGatewayStaticKey passes",
				TestObject: withKey(configurationv1alpha1.EventGatewayStaticKeyRef{Name: "static-key"}),
			},
			{
				Name:                 "a reference with an unsupported kind fails",
				TestObject:           withKey(configurationv1alpha1.EventGatewayStaticKeyRef{Kind: "EventGatewaySchemaRegistry", Name: "static-key"}),
				ExpectedErrorMessage: new(`supported values: "EventGatewayStaticKey"`),
			},
			{
				Name:                 "a reference with an empty name fails",
				TestObject:           withKey(configurationv1alpha1.EventGatewayStaticKeyRef{}),
				ExpectedErrorMessage: new("Required value"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("schema validation schema registry reference", func(t *testing.T) {
		policy := func(schemaRegistry string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
			obj := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{ObjectMeta: common.CommonObjectMeta(ns.Name)}
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
      valueValidationAction: reject
      schemaRegistry: `+schemaRegistry), &obj.Spec.APISpec))
			return obj
		}
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name:       "a reference to an EventGatewaySchemaRegistry passes",
				TestObject: policy(`{name: orders-registry}`),
			},
			{
				Name:       "a reference with its kind and namespace passes",
				TestObject: policy(`{kind: EventGatewaySchemaRegistry, name: orders-registry, namespace: ` + ns.Name + `}`),
			},
			{
				Name:                 "a reference with an unsupported kind fails",
				TestObject:           policy(`{kind: EventGatewayStaticKey, name: orders-registry}`),
				ExpectedErrorMessage: new(`supported values: "EventGatewaySchemaRegistry"`),
			},
			{
				Name:                 "a reference with an empty name fails",
				TestObject:           policy(`{name: ""}`),
				ExpectedErrorMessage: new("should be at least 1 chars long"),
			},
		}.RunWithConfig(t, cfg, scheme)
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
          schemaRegistry: {name: orders}
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
