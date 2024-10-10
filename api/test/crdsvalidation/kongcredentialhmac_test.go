package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongCredentialHMAC(t *testing.T) {
	t.Run("updates not allowed for status conditions", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongCredentialHMAC]{
			{
				Name: "consumerRef change is not allowed for Programmed=True",
				TestObject: &configurationv1alpha1.KongCredentialHMAC{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialHMACSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
							Secret:   lo.ToPtr("secret"),
							Username: lo.ToPtr("test-username"),
						},
					},
					Status: configurationv1alpha1.KongCredentialHMACStatus{
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
				},
				Update: func(c *configurationv1alpha1.KongCredentialHMAC) {
					c.Spec.ConsumerRef.Name = "new-consumer"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "consumerRef change is allowed when consumer is not Programmed=True nor APIAuthValid=True",
				TestObject: &configurationv1alpha1.KongCredentialHMAC{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialHMACSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
							Secret:   lo.ToPtr("secret"),
							Username: lo.ToPtr("test-username"),
						},
					},
					Status: configurationv1alpha1.KongCredentialHMACStatus{
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
				},
				Update: func(c *configurationv1alpha1.KongCredentialHMAC) {
					c.Spec.ConsumerRef.Name = "new-consumer"
				},
			},
		}.Run(t)
	})

	t.Run("fields validation", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongCredentialHMAC]{
			{
				Name: "username is required",
				TestObject: &configurationv1alpha1.KongCredentialHMAC{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialHMACSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
							Secret: lo.ToPtr("secret"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.username: Required value"),
			},
			{
				Name: "username is required and no error is expected when it is set",
				TestObject: &configurationv1alpha1.KongCredentialHMAC{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialHMACSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
							Secret:   lo.ToPtr("secret"),
							Username: lo.ToPtr("test-username"),
						},
					},
				},
			},
		}.Run(t)
	})
}
