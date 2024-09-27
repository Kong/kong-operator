package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var keySetAPISpec = testCasesGroup{
	Name: "kongKeySetAPISpec",
	TestCases: []testCase{
		{
			Name: "name must be set",
			KongKeySet: configurationv1alpha1.KongKeySet{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySetSpec{
					KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.name in body should be at least 1 chars long"),
		},
	},
}
