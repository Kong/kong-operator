package testcases

import (
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var fieldsValidation = testCasesGroup{
	Name: "fields validation",
	TestCases: []testCase{
		{
			Name: "username is required",
			KongCredentialHMAC: configurationv1alpha1.KongCredentialHMAC{
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
			KongCredentialHMAC: configurationv1alpha1.KongCredentialHMAC{
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
	},
}
