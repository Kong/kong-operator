package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// wrongTargetsGroupKindTCs are test cases for the group/kind validation of the target references.
var wrongTargetsGroupKindTCs = testCasesGroup{
	Name: "targets group/kind validation",
	TestCases: []testCase{
		{
			Name: "networking.k8s.io/Ingress, as service target",
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
							Kind:  "Ingress",
							Group: "networking.k8s.io",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("group/kind not allowed for the serviceRef"),
		},
		{
			Name: "core/Service, as route target",
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
							Kind:  "Service",
							Group: "core",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("group/kind not allowed for the routeRef"),
		},
	},
}
