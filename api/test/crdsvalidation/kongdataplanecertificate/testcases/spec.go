package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var spec = testCasesGroup{
	Name: "spec",
	TestCases: []testCase{
		{
			Name: "valid KongDataPlaneClientCertificate",
			KongDataPlaneClientCertificate: configurationv1alpha1.KongDataPlaneClientCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
					KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
		},
		{
			Name: "cert is required",
			KongDataPlaneClientCertificate: configurationv1alpha1.KongDataPlaneClientCertificate{
				ObjectMeta: commonObjectMeta,
			},
			ExpectedErrorMessage: lo.ToPtr("spec.cert in body should be at least 1 chars long"),
		},
		{
			Name: "cert can be altered before programmed",
			KongDataPlaneClientCertificate: configurationv1alpha1.KongDataPlaneClientCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
					KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			KongDataPlaneClientCertificateStatus: &configurationv1alpha1.KongDataPlaneClientCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionFalse,
						Reason:             "Pending",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(k *configurationv1alpha1.KongDataPlaneClientCertificate) {
				k.Spec.Cert = "cert2"
			},
		},
		{
			Name: "cert becomes immutable after programmed",
			KongDataPlaneClientCertificate: configurationv1alpha1.KongDataPlaneClientCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
					KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			KongDataPlaneClientCertificateStatus: &configurationv1alpha1.KongDataPlaneClientCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(k *configurationv1alpha1.KongDataPlaneClientCertificate) {
				k.Spec.Cert = "cert2"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.cert is immutable when an entity is already Programmed"),
		},
	},
}
