package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// pluginRefTCs are test cases for the pluginRef field.
var pluginRefTCs = kpbTestCasesGroup{
	Name: "pluginRef validation",
	TestCases: []kpbTestCase{
		{
			Name: "no plugin reference",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-service",
							Kind:  "Service",
							Group: "core",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("pluginRef name must be set"),
		},
		{
			Name: "empty plugin reference",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-service",
							Kind:  "Service",
							Group: "core",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("pluginRef name must be set"),
		},
		{
			Name: "valid KongPlugin reference",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "test-plugin",
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
			Name: "valid KongClusterPlugin reference",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "test-plugin",
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
			Name: "wrong plugin kind",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("WrongPluginKind"),
						Name: "test-plugin",
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
			ExpectedErrorMessage: lo.ToPtr(`spec.pluginRef.kind: Unsupported value: "WrongPluginKind"`),
		},
	},
}
