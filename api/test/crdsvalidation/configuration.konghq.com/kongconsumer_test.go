package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
)

func TestKongConsumer(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1.KongConsumer{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongConsumer",
				APIVersion: configurationv1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta,
			Username:   "username-1",
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, obj, common.SupportedByKIC, common.ControlPlaneRefNotRequired).Run(t)
	})

	t.Run("required fields", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1.KongConsumer]{
			{
				Name: "username or custom_id required (username provided)",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					Username: "username-1",
				},
			},
			{
				Name: "username or custom_id required (custom_id provided)",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					CustomID: "customid-1",
				},
			},
			{
				Name: "username or custom_id required (none provided)",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Need to provide either username or custom_id"),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1.KongConsumer]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: common.CommonObjectMeta,
					Username:   "username-1",
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
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
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: common.CommonObjectMeta,
					Username:   "username-1",
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
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
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: common.CommonObjectMeta,
					Username:   "username-1",
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
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
