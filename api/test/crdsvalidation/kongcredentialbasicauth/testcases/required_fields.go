package testcases

import (
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var requiredFields = testCasesGroup{
	Name: "required fields validation",
	TestCases: []testCase{
		{
			Name: "password is required",
			KongCredentialBasicAuth: configurationv1alpha1.KongCredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
						Username: "username",
					},
				},
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerREf is immutable when an entity is already Programmed"),
		},
		{
			Name: "username is required",
			KongCredentialBasicAuth: configurationv1alpha1.KongCredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
						Password: "password",
					},
				},
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerREf is immutable when an entity is already Programmed"),
		},
		{
			Name: "password and username are required",
			KongCredentialBasicAuth: configurationv1alpha1.KongCredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
						Username: "username",
						Password: "password",
					},
				},
			},
		},
	},
}
