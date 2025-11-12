package configuration_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKongPluginBindings(t *testing.T) {
	validTestCPRef := func() commonv1alpha1.ControlPlaneRef {
		return commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-control-plane",
			},
		}
	}

	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongPluginBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongPluginBinding",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta,
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				PluginReference: configurationv1alpha1.PluginRef{
					Name: "rate-limiting",
				},
				Targets: &configurationv1alpha1.KongPluginBindingTargets{
					ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
						Name:  "test-service",
						Kind:  "KongService",
						Group: "configuration.konghq.com",
					},
				},
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, obj, common.NotSupportedByKIC, common.ControlPlaneRefRequired).Run(t)
	})

	t.Run("plugin ref", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongPluginBinding]{
			{
				Name: "no plugin reference",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Service",
								Group: "core",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("pluginRef name must be set"),
			},
			{
				Name: "empty plugin reference",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Service",
								Group: "core",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("pluginRef name must be set"),
			},
			{
				Name: "valid KongPlugin reference",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "test-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Service",
								Group: "core",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "valid KongClusterPlugin reference",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "test-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Service",
								Group: "core",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "wrong plugin kind",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("WrongPluginKind"),
							Name: "test-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Service",
								Group: "core",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr(`spec.pluginRef.kind: Unsupported value: "WrongPluginKind"`),
			},
		}.Run(t)
	})

	t.Run("target combinations", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongPluginBinding]{
			{
				Name: "consumer, route, service targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "consumerGroup, route, service targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "consumer, route targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "consumer, service targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "consumerGroup, route targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "consumerGroup, service targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "route, service targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "consumer target",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "consumerGroup target",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "route target",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "service target",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Service",
								Group: "core",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "kongConsumer, kongConsumerGroup, service, route targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
			},
			{
				Name: "kongConsumer, kongConsumerGroup, route targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
			},
			{
				Name: "kongConsumer, kongConsumerGroup, service targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
			},
			{
				Name: "kongConsumer, kongConsumerGroup targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ConsumerReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer",
							},
							ConsumerGroupReference: &configurationv1alpha1.TargetRef{
								Name: "test-consumer-group",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
			},
			{
				Name: "no targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("At least one target reference must be set when scope is 'OnlyTargets'"),
			},
			{
				Name: "empty targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets:         &configurationv1alpha1.KongPluginBindingTargets{},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("At least one target reference must be set when scope is 'OnlyTargets'"),
			},
		}.Run(t)
	})

	t.Run("targets group/kind", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongPluginBinding]{
			{
				Name: "networking.k8s.io/Ingress, as service target",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Ingress",
								Group: "networking.k8s.io",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("group/kind not allowed for the serviceRef"),
			},
			{
				Name: "core/Service, as route target",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "Service",
								Group: "core",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("group/kind not allowed for the routeRef"),
			},
		}.Run(t)
	})

	t.Run("cross targets validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongPluginBinding]{
			{
				Name: "core/Service, configuration.konghq.com/KongRoute targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "Service",
								Group: "core",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "KongRoute",
								Group: "configuration.konghq.com",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr(" KongRoute can be used only when serviceRef is unset or set to KongService"),
			},
			{
				Name: "configuration.konghq.com/KongService, networking.k8s.io/Ingress targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						Targets: &configurationv1alpha1.KongPluginBindingTargets{
							ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-service",
								Kind:  "KongService",
								Group: "configuration.konghq.com",
							},
							RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
								Name:  "test-route",
								Kind:  "Ingress",
								Group: "networking.k8s.io",
							},
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("KongService can be used only when routeRef is unset or set to KongRoute"),
			},
		}.Run(t)
	})

	t.Run("scope=GlobalInControlPlane validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongPluginBinding]{
			{
				Name: "GlobalInControlPlane allow nil targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						Scope: configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane,
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						ControlPlaneRef: validTestCPRef(),
					},
				},
			},
			{
				Name: "GlobalInControlPlane rejects non-nil targets",
				TestObject: &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						Scope: configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane,
						PluginReference: configurationv1alpha1.PluginRef{
							Kind: lo.ToPtr("KongPlugin"),
							Name: "my-plugin",
						},
						ControlPlaneRef: validTestCPRef(),
						Targets:         &configurationv1alpha1.KongPluginBindingTargets{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("No targets must be set when scope is 'GlobalInControlPlane'"),
			},
		}.Run(t)
	})
}
