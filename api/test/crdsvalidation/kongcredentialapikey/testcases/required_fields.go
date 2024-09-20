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
			Name: "key is required",
			KongCredentialAPIKey: configurationv1alpha1.KongCredentialAPIKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					KongCredentialAPIKeyAPISpec: configurationv1alpha1.KongCredentialAPIKeyAPISpec{
						Key: "key",
					},
				},
			},
		},
		{
			Name: "key is required and error is returned if not set",
			KongCredentialAPIKey: configurationv1alpha1.KongCredentialAPIKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
				},
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("key is required"),
		},
	},
}
