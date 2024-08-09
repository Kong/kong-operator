package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// globalTargetTCs are test cases for the combinations of global and targets fields.
var globalTargetTCs = kpbTestCasesGroup{
	Name: "global and targets validation",
	TestCases: []kpbTestCase{
		{
			Name: "global",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "test-plugin",
					},
					Global: lo.ToPtr(true),
				},
			},
		},
		{
			Name: "no global, no targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("When global is unset, target refs have to be used"),
		},
		{
			Name: "false global, no targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Global: lo.ToPtr(false),
				},
			},
			ExpectedErrorMessage: lo.ToPtr("When global is unset, target refs have to be used"),
		},
		{
			Name: "global with targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "my-plugin",
					},
					Global: lo.ToPtr(true),
					Targets: &configurationv1alpha1.KongPluginBindingTargets{
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-service",
							Kind:  "Service",
							Group: "core",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("When global is set, target refs cannot be used"),
		},
	},
}
