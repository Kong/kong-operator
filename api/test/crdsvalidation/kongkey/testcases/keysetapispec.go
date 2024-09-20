package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var keySetAPISpec = testCasesGroup{
	Name: "kongKeyAPISpec",
	TestCases: []testCase{
		{
			Name: "KID must be set",
			KongKey: configurationv1alpha1.KongKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySpec{
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						JWK: lo.ToPtr("{}"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.kid in body should be at least 1 chars long"),
		},
		{
			Name: "one of JWK or PEM must be set",
			KongKey: configurationv1alpha1.KongKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySpec{
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID: "1",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Either 'jwk' or 'pem' must be set"),
		},
	},
}
