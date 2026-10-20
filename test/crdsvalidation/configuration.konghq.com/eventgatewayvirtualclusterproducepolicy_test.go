package configuration_test

import (
	"testing"

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
}
