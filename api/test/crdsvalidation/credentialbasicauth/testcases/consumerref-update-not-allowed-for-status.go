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
			CredentialBasicAuth: configurationv1alpha1.CredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.CredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					CredentialBasicAuthAPISpec: configurationv1alpha1.CredentialBasicAuthAPISpec{
						Password: "password",
						Username: "username",
					},
				},
			},
			CredentialBasicAuthStatus: &configurationv1alpha1.CredentialBasicAuthStatus{
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
			Update: func(c *configurationv1alpha1.CredentialBasicAuth) {
				c.Spec.ConsumerRef.Name = "new-consumer"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "consumerRef change is allowed when consumer is not Programmed=True nor APIAuthValid=True",
			CredentialBasicAuth: configurationv1alpha1.CredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.CredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					CredentialBasicAuthAPISpec: configurationv1alpha1.CredentialBasicAuthAPISpec{
						Password: "password",
						Username: "username",
					},
				},
			},
			CredentialBasicAuthStatus: &configurationv1alpha1.CredentialBasicAuthStatus{
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
			Update: func(c *configurationv1alpha1.CredentialBasicAuth) {
				c.Spec.ConsumerRef.Name = "new-consumer"
			},
		},
	},
}
