package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// updatesNotAllowedForStatus are test cases checking if updates to cpRef
// are indeed blocked for some status conditions.
var updatesNotAllowedForStatus = testCasesGroup{
	Name: "updates not allowed for status conditions",
	TestCases: []testCase{
		{
			Name: "cpRef change is not allowed for Programmed=True",
			KongConsumer: configurationv1.KongConsumer{
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
			KongConsumerStatus: &configurationv1.KongConsumerStatus{
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
			Update: func(c *configurationv1.KongConsumer) {
				c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "cpRef change is allowed when cp is not Programmed=True nor APIAuthValid=True",
			KongConsumer: configurationv1.KongConsumer{
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
			},
			KongConsumerStatus: &configurationv1.KongConsumerStatus{
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
			Update: func(c *configurationv1.KongConsumer) {
				c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
		},
		{
			Name: "updates with Programmed = True when no cpRef is allowed",
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Username:   "username-4",
			},
			KongConsumerStatus: &configurationv1.KongConsumerStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionFalse,
						Reason:             "NotProgrammed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(c *configurationv1.KongConsumer) {
				c.Credentials = []string{"new-credentials"}
			},
		},
	},
}
