package testcases

import (
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// updatesNotAllowedForStatus are test cases checking if updates to consumerRef
// are indeed blocked for some status conditions.
var updatesNotAllowedForStatus = testCasesGroup{
	Name: "updates not allowed for status conditions",
	TestCases: []testCase{
		{
			Name: "consumerRef change is not allowed for Programmed=True",
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialJWTSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
						Key: lo.ToPtr("key"),
					},
				},
			},
			KongCredentialJWTStatus: &configurationv1alpha1.KongCredentialJWTStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndConsumerRefs{},
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Valid",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(c *configurationv1alpha1.KongCredentialJWT) {
				c.Spec.ConsumerRef.Name = "new-consumer"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "consumerRef change is allowed when consumer is not Programmed=True nor APIAuthValid=True",
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialJWTSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
						Key: lo.ToPtr("key"),
					},
				},
			},
			KongCredentialJWTStatus: &configurationv1alpha1.KongCredentialJWTStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndConsumerRefs{},
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionFalse,
						Reason:             "Invalid",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(c *configurationv1alpha1.KongCredentialJWT) {
				c.Spec.ConsumerRef.Name = "new-consumer"
			},
		},
	},
}
