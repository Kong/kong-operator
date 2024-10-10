package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongCredentialJWT(t *testing.T) {
	t.Run("updates not allowed for status conditions", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongCredentialJWT]{
			{
				Name: "consumerRef change is not allowed for Programmed=True",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key: lo.ToPtr("key"),
						},
					},
					Status: configurationv1alpha1.KongCredentialJWTStatus{
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
				Update: func(c *configurationv1alpha1.KongCredentialJWT) {
					c.Spec.ConsumerRef.Name = "new-consumer"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "consumerRef change is allowed when consumer is not Programmed=True nor APIAuthValid=True",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key: lo.ToPtr("key"),
						},
					},
					Status: configurationv1alpha1.KongCredentialJWTStatus{
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
				Update: func(c *configurationv1alpha1.KongCredentialJWT) {
					c.Spec.ConsumerRef.Name = "new-consumer"
				},
			},
		}.Run(t)
	})

	t.Run("fields validation", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongCredentialJWT]{
			{
				Name: "rsa_public_key is required when algorithm is RS256",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmRs384),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is RS384",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmRs384),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is RS512",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmRs512),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is PS256",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmPs384),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is PS384",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmPs384),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is PS512",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmPs512),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is ES256",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmEs384),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is ES384",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmEs384),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is ES512",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmEs512),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is required when algorithm is EdDSA",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmEdDsa),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"),
			},
			{
				Name: "rsa_public_key is not required when algorithm is Hs256",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmHs256),
						},
					},
				},
			},
			{
				Name: "rsa_public_key is not required when algorithm is Hs384",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmHs384),
						},
					},
				},
			},
			{
				Name: "rsa_public_key is not required when algorithm is Hs512",
				TestObject: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "test-kong-consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Key:       lo.ToPtr("key"),
							Algorithm: string(sdkkonnectcomp.AlgorithmHs512),
						},
					},
				},
			},
		}.Run(t)
	})

}
