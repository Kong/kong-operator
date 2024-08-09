package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// crossTargetsTCs are test cases for the xvalidation between the target types.
var crossTargetsTCs = kpbTestCasesGroup{
	Name: "cross targets validation",
	TestCases: []kpbTestCase{
		{
			Name: "core/Service, configuration.konghq.com/KongRoute targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
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
				},
			},
			ExpectedErrorMessage: lo.ToPtr(" KongRoute can be used only when serviceRef is unset or set to KongService"),
		},
		{
			Name: "configuration.konghq.com/KongService, networking.k8s.io/Ingress targets",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
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
				},
			},
			ExpectedErrorMessage: lo.ToPtr("KongService can be used only when routeRef is unset or set to KongRoute"),
		},
	},
}
