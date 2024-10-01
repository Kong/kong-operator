package testcases

import (
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var fieldsValidation = testCasesGroup{
	Name: "fields validation",
	TestCases: []testCase{
		{
			Name: "rsa_public_key is required when algorithm is RS256",
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
			KongCredentialJWT: configurationv1alpha1.KongCredentialJWT{
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
	},
}
