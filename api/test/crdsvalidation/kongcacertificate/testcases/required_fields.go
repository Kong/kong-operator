package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var requiredFields = testCasesGroup{
	Name: "required fields validation",
	TestCases: []testCase{
		{
			Name: "cert field is required",
			KongCACertificate: configurationv1alpha1.KongCACertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCACertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.cert: Required value"),
		},
	},
}
