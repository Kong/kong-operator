package testcases

import (
	"github.com/Kong/sdk-konnect-go/models/components"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/samber/lo"
)

var protocols = testCasesGroup{
	Name: "protocols validation",
	TestCases: []testCase{
		{
			Name: "no http in protocols implies no other requirements",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type:          configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{Name: "svc"},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Paths: []string{"/"},
					},
				},
			},
		},
		{
			Name: "http in protocols with hosts set yields no error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type:          configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{Name: "svc"},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Protocols: []components.RouteProtocols{"http"},
						Hosts:     []string{"example.com"},
					},
				},
			},
		},
		{
			Name: "http in protocols no hosts, methods, paths or headers yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type:          configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{Name: "svc"},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Protocols: []components.RouteProtocols{"http"},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("If protocols has 'http', at least one of 'hosts', 'methods', 'paths' or 'headers' must be set"),
		},
	},
}
