package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongSNI(t *testing.T) {
	t.Run("certificate ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongSNI]{
			{
				Name: "certificate ref name is required",
				TestObject: &configurationv1alpha1.KongSNI{
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
				TestObject: &configurationv1alpha1.KongSNI{
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
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: configurationv1alpha1.KongObjectRef{
							Name: "cert1",
						},
						KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
							Name: "example.com",
						},
					},
					Status: configurationv1alpha1.KongSNIStatus{
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
				},
				Update: func(sni *configurationv1alpha1.KongSNI) {
					sni.Spec.CertificateRef = configurationv1alpha1.KongObjectRef{
						Name: "cert-2",
					}
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.certificateRef is immutable when programmed"),
			},
		}.Run(t)
	})

	t.Run("spec", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongSNI]{
			{
				Name: "spec.name must not be empty",
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: configurationv1alpha1.KongObjectRef{
							Name: "cert1",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.name in body should be at least 1 chars long"),
			},
		}.Run(t)
	})
}
