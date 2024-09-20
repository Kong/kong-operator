package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var keySetRef = testCasesGroup{
	Name: "keySetRef",
	TestCases: []testCase{
		{
			Name: "when type is 'namespacedRef', namespacedRef is required",
			KongKey: configurationv1alpha1.KongKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySpec{
					KeySetRef: &configurationv1alpha1.KeySetRef{
						Type: configurationv1alpha1.KeySetRefNamespacedRef,
					},
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID: "1",
						JWK: lo.ToPtr("{}"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is namespacedRef, namespacedRef must be set"),
		},
		{
			Name: "'namespacedRef' type is accepted",
			KongKey: configurationv1alpha1.KongKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySpec{
					KeySetRef: &configurationv1alpha1.KeySetRef{
						Type: configurationv1alpha1.KeySetRefNamespacedRef,
						NamespacedRef: &configurationv1alpha1.KeySetNamespacedRef{
							Name: "keyset-1",
						},
					},
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID: "1",
						JWK: lo.ToPtr("{}"),
					},
				},
			},
		},
		{
			Name: "'konnectID' type is accepted",
			KongKey: configurationv1alpha1.KongKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySpec{
					KeySetRef: &configurationv1alpha1.KeySetRef{
						Type:      configurationv1alpha1.KeySetRefKonnectID,
						KonnectID: lo.ToPtr("keyset-1"),
					},
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID: "1",
						JWK: lo.ToPtr("{}"),
					},
				},
			},
		},
		{
			Name: "when type is 'konnectID', konnectID is required",
			KongKey: configurationv1alpha1.KongKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySpec{
					KeySetRef: &configurationv1alpha1.KeySetRef{
						Type: configurationv1alpha1.KeySetRefKonnectID,
					},
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID: "1",
						JWK: lo.ToPtr("{}"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		},
		{
			Name: "unknown type is not accepted",
			KongKey: configurationv1alpha1.KongKey{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongKeySpec{
					KeySetRef: &configurationv1alpha1.KeySetRef{
						Type: configurationv1alpha1.KeySetRefType("unknown"),
					},
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID: "1",
						JWK: lo.ToPtr("{}"),
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr(`Unsupported value: "unknown": supported values: "konnectID", "namespacedRef"`),
		},
	},
}
