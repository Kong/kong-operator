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
			Name: "group is required",
			KongCredentialACL: configurationv1alpha1.KongCredentialACL{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialACLSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
					KongCredentialACLAPISpec: configurationv1alpha1.KongCredentialACLAPISpec{
						Group: "group1",
					},
				},
			},
		},
		{
			Name: "group is required and error is returned if not set",
			KongCredentialACL: configurationv1alpha1.KongCredentialACL{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCredentialACLSpec{
					ConsumerRef: corev1.LocalObjectReference{
						Name: "test-kong-consumer",
					},
				},
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("group is required"),
		},
	},
}
