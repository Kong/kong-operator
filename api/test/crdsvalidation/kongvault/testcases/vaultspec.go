package testcases

import (
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/samber/lo"
)

var vaultSpec = testCasesGroup{
	Name: "vault specification",
	TestCases: []testCase{
		{
			Name: "backend must be non-empty",
			KongVault: configurationv1alpha1.KongVault{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongVaultSpec{
					Prefix: "aws-vault",
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.backend: Invalid value"),
		},
		{
			Name: "prefix must be non-empty",
			KongVault: configurationv1alpha1.KongVault{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend: "aws",
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.prefix: Invalid value"),
		},
		{
			Name: "prefix is immutatble",
			KongVault: configurationv1alpha1.KongVault{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend: "aws",
					Prefix:  "aws-vault",
				},
			},
			Update: func(v *configurationv1alpha1.KongVault) {
				v.Spec.Prefix = v.Spec.Prefix + "-1"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("The spec.prefix field is immutable"),
		},
	},
}
