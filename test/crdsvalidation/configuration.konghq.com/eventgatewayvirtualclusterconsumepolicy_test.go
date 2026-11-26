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
				Name:       "the name of the object passes",
				TestObject: policy(`{name: orders}`),
			},
			{
				Name:       "the name, kind and namespace of the object pass",
				TestObject: policy(`{kind: EventGatewaySchemaRegistry, name: orders, namespace: ` + ns.Name + `}`),
			},
			{
				Name:                 "another kind fails",
				TestObject:           policy(`{kind: EventGatewayStaticKey, name: orders}`),
				ExpectedErrorMessage: new(`supported values: "EventGatewaySchemaRegistry"`),
			},
			{
				Name:                 "an empty name fails",
				TestObject:           policy(`{name: ""}`),
				ExpectedErrorMessage: new("should be at least 1 chars long"),
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
          schemaRegistry: {name: orders}
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
