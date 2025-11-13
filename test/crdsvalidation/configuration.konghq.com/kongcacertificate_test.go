package configuration_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
	"github.com/kong/kong-operator/test/envtest"
)

func TestKongCACertificate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, _ := envtest.Setup(t, ctx, scheme)

	t.Run("required fields validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongCACertificate]{
			{
				Name: "cert field is required",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCACertificateSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert: Required value"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongCACertificate{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongCACertificate",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta,
			Spec: configurationv1alpha1.KongCACertificateSpec{
				KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
					Cert: "cert",
				},
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, obj, common.NotSupportedByKIC, common.ControlPlaneRefRequired).Run(t)
	})
}
