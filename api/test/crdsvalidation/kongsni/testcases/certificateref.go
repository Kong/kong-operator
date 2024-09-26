package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var certificateRef = testCasesGroup{
	Name: "certificateRef",
	TestCases: []testCase{
		{
			Name: "certificate ref name is required",
			KongSNI: configurationv1alpha1.KongSNI{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: configurationv1alpha1.KongObjectRef{},
					KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
						Name: "example.com",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.certificateRef.name in body should be at least 1 chars long"),
		},
		{
			Name: "certificate ref can be changed before programmed",
			KongSNI: configurationv1alpha1.KongSNI{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: configurationv1alpha1.KongObjectRef{
						Name: "cert1",
					},
					KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
						Name: "example.com",
					},
				},
			},
			Update: func(sni *configurationv1alpha1.KongSNI) {
				sni.Spec.CertificateRef = configurationv1alpha1.KongObjectRef{
					Name: "cert-2",
				}
			},
		},
		{
			Name: "certiifacate ref is immutable after programmed",
			KongSNI: configurationv1alpha1.KongSNI{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: configurationv1alpha1.KongObjectRef{
						Name: "cert1",
					},
					KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
						Name: "example.com",
					},
				},
			},
			KongSNIStatus: &configurationv1alpha1.KongSNIStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "programmed",
						ObservedGeneration: int64(1),
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(sni *configurationv1alpha1.KongSNI) {
				sni.Spec.CertificateRef = configurationv1alpha1.KongObjectRef{
					Name: "cert-2",
				}
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.certificateRef is immutable when programmed"),
		},
	},
}
