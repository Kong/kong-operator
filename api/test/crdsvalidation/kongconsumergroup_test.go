package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongConsumerGroup(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1beta1.KongConsumerGroup]{
			{
				// Since KongConsumerGroups managed by KIC do not require spec.controlPlane, KongConsumerGroups without spec.controlPlaneRef should be allowed.
				Name: "no CPRef is valid",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						Name: "test",
					},
				},
			},
			{
				Name: "cpRef cannot have namespace",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "test-konnect-control-plane",
								Namespace: "another-namespace",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
			},
			{
				Name: "providing konnectID when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
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
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
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
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
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
				TestObject: &configurationv1beta1.KongConsumerGroup{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1beta1.KongConsumerGroupSpec{
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
}
