package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var cpRef = testCasesGroup{
	Name: "cp ref validation",
	TestCases: []testCase{
		{
			Name: "cannot specify with service ref",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
							Name: "test-konnect-service",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Only one of controlPlaneRef or serviceRef can be set"),
		},
		{
			Name: "konnectNamespacedRef reference is valid",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
				},
			},
		},
		{
			Name: "not providing konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		},
		{
			Name: "not providing konnectID when type is konnectID yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectID,
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		},
		{
			Name: "providing namespace in konnectNamespacedRef yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name:      "test-konnect-control-plane",
							Namespace: "another-namespace",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
		},
		{
			Name: "providing konnectID when type is konnectNamespacedRef yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectID: lo.ToPtr("123456"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		},

		{
			Name: "providing konnectNamespacedRef when type is konnectID yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		},
		{
			Name: "providing konnectNamespacedRef and konnectID when type is konnectID yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("123456"),
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectNamespacedRef must not be set"),
		},
		{
			Name: "providing konnectID and konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectID: lo.ToPtr("123456"),
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectID must not be set"),
		},
		{
			Name: "konnectNamespacedRef reference name cannot be changed when an entity is Programmed",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
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
				ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
			KongRoute: configurationv1alpha1.KongRoute{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongRouteSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
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
				ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
	},
}
