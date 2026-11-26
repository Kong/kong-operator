package configuration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	t.Run("Chainsaw generated fixtures", func(t *testing.T) {
		cl, err := client.New(cfg, client.Options{Scheme: scheme})
		require.NoError(t, err)
		dir := t.TempDir()
		applied := filepath.Join(dir, "applied.json")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(`#!/bin/bash
set -euo pipefail
[[ "$*" == "apply -f -" ]]
cat > "$APPLIED_FILE"
`), 0o600))
		require.NoError(t, os.Chmod(filepath.Join(dir, "kubectl"), 0o700))
		for _, tt := range []struct {
			name, policyType, parent string
		}{
			{name: "parse-a", policyType: "schemaValidation"},
			{name: "child", policyType: "modifyHeaders", parent: "parse-a"},
			{name: "encrypted", policyType: "encryptFields", parent: "parse-a"},
		} {
			t.Run(tt.policyType, func(t *testing.T) {
				cmd := exec.CommandContext(t.Context(), "bash", "-c",
					`source scripts/chain.sh; create_policy "$FIXTURE_NAME" "$FIXTURE_TYPE" "$FIXTURE_PARENT"`)
				cmd.Dir = filepath.Join("..", "..", "e2e", "chainsaw", "eventgateway", "producepolicychain-lifecycle")
				cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
					"NAMESPACE="+ns.Name, "VIRTUAL_CLUSTER_NAME=fixture-vc", "APPLIED_FILE="+applied,
					"FIXTURE_NAME="+tt.name, "FIXTURE_TYPE="+tt.policyType, "FIXTURE_PARENT="+tt.parent)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				raw, err := os.ReadFile(applied)
				require.NoError(t, err)
				var obj unstructured.Unstructured
				require.NoError(t, obj.UnmarshalJSON(raw))
				require.NoError(t, cl.Create(ctx, &obj, client.FieldValidation(metav1.FieldValidationStrict)))

				var stored configurationv1alpha1.EventGatewayVirtualClusterProducePolicy
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(&obj), &stored))
				require.Equal(t, "fixture-vc", stored.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name)
				require.Equal(t, tt.parent, stored.Spec.APISpec.ParentPolicyRef.Name)
				if tt.policyType != "encryptFields" {
					return
				}
				config := stored.Spec.APISpec.ParsedRecordEncryptFieldsPolicyCreate.Config
				require.Equal(t, configurationv1alpha1.ProduceFailureMode("reject"), config.FailureMode)
				require.Len(t, config.EncryptFields, 1)
				require.NotNil(t, config.EncryptFields[0].Paths.Array)
				require.Equal(t, configurationv1alpha1.EventGatewayParsedRecordFieldPathsArray{
					{Match: "$.secret"},
				}, *config.EncryptFields[0].Paths.Array)
				require.Equal(t, configurationv1alpha1.EventGatewayStaticKeyRef{
					Kind: "EventGatewayStaticKey", Name: "chain-key",
				}, config.EncryptFields[0].EncryptionKey.Static.Key)

				// Strict admission must reject the former ObjectRef wrapper, not silently prune it.
				var legacy unstructured.Unstructured
				require.NoError(t, legacy.UnmarshalJSON(raw))
				legacy.SetName("legacy-encrypted")
				fields, found, err := unstructured.NestedSlice(legacy.Object, "spec", "apiSpec", "encryptFields", "config", "encryptFields")
				require.NoError(t, err)
				require.True(t, found)
				require.NoError(t, unstructured.SetNestedMap(fields[0].(map[string]any),
					map[string]any{"namespacedRef": map[string]any{"name": "chain-key"}}, "encryptionKey", "static", "key"))
				require.NoError(t, unstructured.SetNestedSlice(legacy.Object, fields, "spec", "apiSpec", "encryptFields", "config", "encryptFields"))
				err = cl.Create(ctx, &legacy, client.FieldValidation(metav1.FieldValidationStrict))
				require.ErrorContains(t, err, `unknown field "spec.apiSpec.encryptFields.config.encryptFields[0].encryptionKey.static.key.namespacedRef"`)
			})
		}
	})

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
