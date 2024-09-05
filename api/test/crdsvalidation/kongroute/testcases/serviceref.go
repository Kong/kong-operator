package testcases

import (
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var serviceRef = testCasesGroup{
	Name: "service ref validation",
	TestCases: []testCase{
		{
			Name: "NamespacedRef reference is valid",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
							Name: "test-konnect-service",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Paths: []string{"/"},
					},
				},
			},
		},
		{
			Name: "not providing namespacedRef when type is namespacedRef yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Paths: []string{"/"},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is namespacedRef, namespacedRef must be set"),
		},
		{
			Name: "NamespacedRef reference name cannot be changed when an entity is Programmed",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
							Name: "test-konnect-service",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Paths: []string{"/"},
					},
				},
			},
			KongRouteStatus: &configurationv1alpha1.KongRouteStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongRoute) {
				ks.Spec.ServiceRef.NamespacedRef.Name = "new-konnect-service"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.serviceRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "NamespacedRef reference type cannot be changed when an entity is Programmed",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
							Name: "test-konnect-service",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Paths: []string{"/"},
					},
				},
			},
			KongRouteStatus: &configurationv1alpha1.KongRouteStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongRoute) {
				ks.Spec.ServiceRef.Type = "otherRef"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.serviceRef is immutable when an entity is already Programmed"),
		},
	},
}
