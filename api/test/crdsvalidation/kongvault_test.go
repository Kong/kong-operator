package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongVault(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongVault]{
			{
				Name: "no control plane ref to have control plane ref in valid",
				TestObject: &configurationv1alpha1.KongVault{
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
				TestObject: &configurationv1alpha1.KongVault{
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
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongVaultSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
							KonnectID: lo.ToPtr("konnect-1"),
						},
						Backend: "aws",
						Prefix:  "aws-vault",
					},
					Status: configurationv1alpha1.KongVaultStatus{
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
				},
				Update: func(v *configurationv1alpha1.KongVault) {
					v.Spec.ControlPlaneRef = &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("konnect-2"),
					}
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
		}.Run(t)
	})

	t.Run("spec", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongVault]{
			{
				Name: "backend must be non-empty",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongVaultSpec{
						Prefix: "aws-vault",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.backend: Invalid value"),
			},
			{
				Name: "prefix must be non-empty",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.prefix: Invalid value"),
			},
			{
				Name: "prefix is immutatble",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault",
					},
				},
				Update: func(v *configurationv1alpha1.KongVault) {
					v.Spec.Prefix += "-1"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("The spec.prefix field is immutable"),
			},
		}.Run(t)
	})
}
