package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/samber/lo"
)

var cpRef = testCasesGroup{
	Name: "cp ref validation",
	TestCases: []testCase{
		{
			Name: "konnectNamespacedRef reference is valid",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
		},
		{
			Name: "not providing konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		},
		{
			Name: "not providing konnectID when type is konnectID yields an error",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectID,
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		},
		{
			Name: "konnectNamespacedRef reference name cannot be changed when entity is Programmed",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			KongServiceStatus: &configurationv1alpha1.KongServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongService) {
				ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when entity is already Programmed."),
		},
		{
			Name: "konnectNamespacedRef reference type cannot be changed when entity is Programmed",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			KongServiceStatus: &configurationv1alpha1.KongServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongService) {
				ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when entity is already Programmed."),
		},
	},
}
