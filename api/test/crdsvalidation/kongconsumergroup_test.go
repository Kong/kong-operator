package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongConsumerGroup(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1beta1.KongConsumerGroup{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongConsumer",
				APIVersion: configurationv1beta1.GroupVersion.String(),
			},
			ObjectMeta: commonObjectMeta,
			Spec: configurationv1beta1.KongConsumerGroupSpec{
				Name: "group1",
			},
		}

		NewCRDValidationTestCasesGroupCPRefChange(t, obj).Run(t)
	})

	t.Run("cp ref update", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1beta1.KongConsumerGroup]{
			{
				Name: "cpRef change is not allowed for Programmed=True",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					Status: configurationv1beta1.KongConsumerGroupStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{},
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionTrue,
								Reason:             "Valid",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(c *configurationv1beta1.KongConsumerGroup) {
					c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "cpRef change is allowed when cp is not Programmed=True nor APIAuthValid=True",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					Status: configurationv1beta1.KongConsumerGroupStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{},
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionFalse,
								Reason:             "NotProgrammed",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(c *configurationv1beta1.KongConsumerGroup) {
					c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
			},
			{
				Name: "updates with Programmed = True when no cpRef is allowed",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						Name: "group1",
					},
					Status: configurationv1beta1.KongConsumerGroupStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionFalse,
								Reason:             "NotProgrammed",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(c *configurationv1beta1.KongConsumerGroup) {
					c.Spec.Name = "group2"
				},
			},
		}.Run(t)
	})

	t.Run("fields", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1beta1.KongConsumerGroup]{
			{
				Name: "name field can be set",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						Name: "test-consumer-group",
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1beta1.KongConsumerGroup]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						Name: "cg-1",
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						Name: "cg-1",
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						Name: "cg-1",
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
