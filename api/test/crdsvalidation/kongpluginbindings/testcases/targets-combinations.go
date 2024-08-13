// targetsCombinationsTCs are test cases for the various combinations of target references.
package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// targetsCombinationsTCs are test cases for the various combinations of target references.
var targetsCombinationsTCs = testCasesGroup{
	Name: "targets combinations validation",
	TestCases: []testCase{
		{
			Name: "consumer, route, service targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
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
				},
			},
		},
		{
			Name: "consumerGroup, route, service targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
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
				},
			},
		},
		{
			Name: "consumer, route targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ConsumerReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer",
						},
						RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-route",
							Kind:  "KongRoute",
							Group: "configuration.konghq.com",
						},
					},
				},
			},
		},
		{
			Name: "consumer, service targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ConsumerReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer",
						},
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-route",
							Kind:  "KongService",
							Group: "configuration.konghq.com",
						},
					},
				},
			},
		},
		{
			Name: "consumerGroup, route targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ConsumerGroupReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer-group",
						},
						RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-route",
							Kind:  "KongRoute",
							Group: "configuration.konghq.com",
						},
					},
				},
			},
		},
		{
			Name: "consumerGroup, service targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ConsumerGroupReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer-group",
						},
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-route",
							Kind:  "KongService",
							Group: "configuration.konghq.com",
						},
					},
				},
			},
		},
		{
			Name: "route, service targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
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
				},
			},
		},
		{
			Name: "consumer target",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ConsumerReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer",
						},
					},
				},
			},
		},
		{
			Name: "consumerGroup target",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ConsumerGroupReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer",
						},
					},
				},
			},
		},
		{
			Name: "route target",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						RouteReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-route",
							Kind:  "KongRoute",
							Group: "configuration.konghq.com",
						},
					},
				},
			},
		},
		{
			Name: "service target",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-service",
							Kind:  "Service",
							Group: "core",
						},
					},
				},
			},
		},
		{
			Name: "kongConsumer, kongConsumerGroup, service, route targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
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
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
		},
		{
			Name: "kongConsumer, kongConsumerGroup, route targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
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
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
		},
		{
			Name: "kongConsumer, kongConsumerGroup, service targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
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
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
		},
		{
			Name: "kongConsumer, kongConsumerGroup targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ConsumerReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer",
						},
						ConsumerGroupReference: &configurationv1alpha1.TargetRef{
							Name: "test-consumer-group",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Cannot set Consumer and ConsumerGroup at the same time"),
		},
		{
			Name: "no targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("At least one entity reference must be set"),
		},
		{
			Name: "empty targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("At least one entity reference must be set"),
		},
	},
}
