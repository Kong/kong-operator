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
			Name: "providing konnectID when type is konnectNamespacedRef yields an error",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectID: lo.ToPtr("123456"),
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		},

		{
			Name: "providing konnectNamespacedRef when type is konnectID yields an error",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		},
		{
			Name: "providing konnectNamespacedRef and konnectID when type is konnectID yields an error",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("123456"),
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectNamespacedRef must not be set"),
		},
		{
			Name: "providing konnectID and konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectID: lo.ToPtr("123456"),
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectID must not be set"),
		},
		{
			Name: "providing namespace in konnectNamespacedRef yields an error",
			KongService: configurationv1alpha1.KongService{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongServiceSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name:      "test-konnect-control-plane",
							Namespace: "another-namespace",
						},
					},
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Host: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
		},
		{
			Name: "konnectNamespacedRef reference name cannot be changed when an entity is Programmed",
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
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
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
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
	},
}
