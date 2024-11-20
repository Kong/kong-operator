package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongCACertificate(t *testing.T) {
	t.Run("required fields validation", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongCACertificate]{
			{
				Name: "cert field is required",
				TestObject: &configurationv1alpha1.KongCACertificate{
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
		}.Run(t)
	})

	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongCACertificate{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongCACertificate",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: commonObjectMeta,
			Spec: configurationv1alpha1.KongCACertificateSpec{
				KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
					Cert: "cert",
				},
			},
		}

		NewCRDValidationTestCasesGroupCPRefChange(t, obj, NotSupportedByKIC).Run(t)
	})
}
