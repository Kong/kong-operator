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
			CredentialBasicAuth: configurationv1alpha1.CredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.CredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					CredentialBasicAuthAPISpec: configurationv1alpha1.CredentialBasicAuthAPISpec{
						Username: "username",
					},
				},
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerREf is immutable when an entity is already Programmed"),
		},
		{
			Name: "username is required",
			CredentialBasicAuth: configurationv1alpha1.CredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.CredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					CredentialBasicAuthAPISpec: configurationv1alpha1.CredentialBasicAuthAPISpec{
						Password: "password",
					},
				},
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.consumerREf is immutable when an entity is already Programmed"),
		},
		{
			Name: "password and username are required",
			CredentialBasicAuth: configurationv1alpha1.CredentialBasicAuth{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.CredentialBasicAuthSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					CredentialBasicAuthAPISpec: configurationv1alpha1.CredentialBasicAuthAPISpec{
						Username: "username",
						Password: "password",
					},
				},
			},
		},
	},
}
