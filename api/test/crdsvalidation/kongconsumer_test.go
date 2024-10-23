package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongConsumer(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1.KongConsumer]{
			{
				// Since KongConsumers managed by KIC do not require spec.controlPlane, KongConsumers without spec.controlPlaneRef should be allowed.
				Name: "no cpRef is valid",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Username:   "username-1",
				},
			},
			{
				Name: "cpRef cannot have namespace",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "test-konnect-control-plane",
								Namespace: "another-namespace",
							},
						},
					},
					Username: "username-1",
				},
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
			},
			{
				Name: "providing konnectID when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
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
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
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
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
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
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
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
		}.Run(t)
	})

	t.Run("cp ref update", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1.KongConsumer]{
			{
				Name: "cpRef change is not allowed for Programmed=True",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					Username: "username-1",
					Status: configurationv1.KongConsumerStatus{
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
				Update: func(c *configurationv1.KongConsumer) {
					c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "cpRef change is allowed when cp is not Programmed=True nor APIAuthValid=True",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					Username: "username-3",
					Status: configurationv1.KongConsumerStatus{
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
				Update: func(c *configurationv1.KongConsumer) {
					c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
			},
			{
				Name: "updates with Programmed = True when no cpRef is allowed",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Username:   "username-4",
					Status: configurationv1.KongConsumerStatus{
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
				Update: func(c *configurationv1.KongConsumer) {
					c.Credentials = []string{"new-credentials"}
				},
			},
		}.Run(t)
	})

	t.Run("required fields", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1.KongConsumer]{
			{
				Name: "username or custom_id required (username provided)",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1.KongConsumerSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
		CRDValidationTestCasesGroup[*configurationv1.KongConsumer]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Username:   "username-1",
					Spec: configurationv1.KongConsumerSpec{
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
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Username:   "username-1",
					Spec: configurationv1.KongConsumerSpec{
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
				TestObject: &configurationv1.KongConsumer{
					ObjectMeta: commonObjectMeta,
					Username:   "username-1",
					Spec: configurationv1.KongConsumerSpec{
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
