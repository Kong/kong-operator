package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// updatesNotAllowedForStatus are test cases checking if updates to cpRef
// are indeed blocked for some status conditions.
var updatesNotAllowedForStatus = testCasesGroup{
	Name: "updates not allowed for status conditions",
	TestCases: []testCase{
		{
			Name: "cpRef change is not allowed for Programmed=True",
			KongConsumerGroup: configurationv1beta1.KongConsumerGroup{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1beta1.KongConsumerGroupSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
			},
			KongConsumerGroupStatus: &configurationv1beta1.KongConsumerGroupStatus{
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
			Update: func(c *configurationv1beta1.KongConsumerGroup) {
				c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "cpRef change is allowed when cp is not Programmed=True nor APIAuthValid=True",
			KongConsumerGroup: configurationv1beta1.KongConsumerGroup{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1beta1.KongConsumerGroupSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
			},
			KongConsumerGroupStatus: &configurationv1beta1.KongConsumerGroupStatus{
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
			Update: func(c *configurationv1beta1.KongConsumerGroup) {
				c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
		},
		{
			Name: "updates with Programmed = True when no cpRef is allowed",
			KongConsumerGroup: configurationv1beta1.KongConsumerGroup{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1beta1.KongConsumerGroupSpec{
					Name: "group1",
				},
			},
			KongConsumerGroupStatus: &configurationv1beta1.KongConsumerGroupStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionFalse,
						Reason:             "NotProgrammed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(c *configurationv1beta1.KongConsumerGroup) {
				c.Spec.Name = "group2"
			},
		},
	},
}
