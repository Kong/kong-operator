package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

func TestKongVault(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongVault{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongVault",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: commonObjectMeta,
			Spec: configurationv1alpha1.KongVaultSpec{
				Backend: "aws",
				Prefix:  "aws-vault",
			},
		}

		NewCRDValidationTestCasesGroupCPRefChange(t, obj, SupportedByKIC, ControlPlaneRefNotRequired).Run(t)
	})

	t.Run("spec", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongVault]{
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

	t.Run("tags validation", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongVault]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongVaultSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Backend: "aws",
						Prefix:  "aws-vault",
						Tags: func() []string {
							var tags []string
							for i := range 20 {
								tags = append(tags, fmt.Sprintf("tag-%d", i))
							}
							return tags
						}(),
					},
				},
			},
			{
				Name: "more than 20 tags are not allowed",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongVaultSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Backend: "aws",
						Prefix:  "aws-vault",
						Tags: func() []string {
							var tags []string
							for i := range 21 {
								tags = append(tags, fmt.Sprintf("tag-%d", i))
							}
							return tags
						}(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.tags: Too many: 21: must have at most 20 items"),
			},
			{
				Name: "tags entries must not be longer than 128 characters",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongVaultSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Backend: "aws",
						Prefix:  "aws-vault",
						Tags: []string{
							lo.RandomString(129, lo.AlphanumericCharset),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("tags entries must not be longer than 128 characters"),
			},
		}.Run(t)
	})
}
