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
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Key: "test-key",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.cert: Required value"),
		},
		{
			Name: "key field is required",
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "test-cert",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.key: Required value"),
		},
		{
			Name: "cert and key fields are required",
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "test-cert",
						Key:  "test-key",
					},
				},
			},
		},
	},
}
