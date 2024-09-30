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
			Name: "valid KongDataplaneCertificate",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
		},
		{
			Name: "cert is required",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
			},
			ExpectedErrorMessage: lo.ToPtr("spec.cert in body should be at least 1 chars long"),
		},
		{
			Name: "cert can be altered before programmed",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			KongDataplaneCertificateStatus: &configurationv1alpha1.KongDataplaneCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionFalse,
						Reason:             "Pending",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(k *configurationv1alpha1.KongDataplaneCertificate) {
				k.Spec.Cert = "cert2"
			},
		},
		{
			Name: "cert becomes immutable after programmed",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			KongDataplaneCertificateStatus: &configurationv1alpha1.KongDataplaneCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(k *configurationv1alpha1.KongDataplaneCertificate) {
				k.Spec.Cert = "cert2"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.cert is immutable when an entity is already Programmed"),
		},
	},
}
