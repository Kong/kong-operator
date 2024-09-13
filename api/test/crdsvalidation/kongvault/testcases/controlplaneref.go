package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

var controlPlaneRef = testCasesGroup{
	Name: "control plane ref",
	TestCases: []testCase{
		{
			Name: "no control plane ref to have control plane ref in valid",
			KongVault: configurationv1alpha1.KongVault{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend: "aws",
					Prefix:  "aws-vault",
				},
			},
			Update: func(v *configurationv1alpha1.KongVault) {
				v.Spec.ControlPlaneRef = &configurationv1alpha1.ControlPlaneRef{
					Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
					KonnectID: lo.ToPtr("konnect-1"),
				}
			},
		},
		{
			Name: "have control plane to no control plane is invalid",
			KongVault: configurationv1alpha1.KongVault{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongVaultSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("konnect-1"),
					},
					Backend: "aws",
					Prefix:  "aws-vault",
				},
			},
			Update: func(v *configurationv1alpha1.KongVault) {
				v.Spec.ControlPlaneRef = nil
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("controlPlaneRef is required once set"),
		},
		{
			Name: "control plane is immutable once programmed",
			KongVault: configurationv1alpha1.KongVault{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongVaultSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("konnect-1"),
					},
					Backend: "aws",
					Prefix:  "aws-vault",
				},
			},
			KongVaultStatus: &configurationv1alpha1.KongVaultStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
					ControlPlaneID: "konnect-1",
				},
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(v *configurationv1alpha1.KongVault) {
				v.Spec.ControlPlaneRef = &configurationv1alpha1.ControlPlaneRef{
					Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
					KonnectID: lo.ToPtr("konnect-2"),
				}
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
	},
}
