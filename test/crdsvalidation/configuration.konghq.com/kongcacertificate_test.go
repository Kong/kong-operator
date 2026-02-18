package configuration_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKongCACertificate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongCACertificate]{
			{
				Name: "type=inline requires cert field",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
							Cert: "test-cert",
						},
					},
				},
			},
			{
				Name: "type=inline with missing cert returns error",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert is required when type is 'inline'"),
			},
			{
				Name: "type=inline with empty cert returns error",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
							Cert: "",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert is required when type is 'inline'"),
			},
			{
				Name: "type=secretRef requires secretRef field",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
					},
				},
			},
			{
				Name: "type=secretRef without secretRef returns error",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.secretRef is required when type is 'secretRef'"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("mixing inline and secretRef validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongCACertificate]{
			{
				Name: "cert cannot be mixed with secretRef",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
						KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
							Cert: "test-cert",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cert and secretRef cannot be set at the same time"),
			},
			{
				Name: "valid: secretRef alone is allowed",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
					},
				},
			},
			{
				Name: "valid: cert alone is allowed for inline",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
							Cert: "test-cert",
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("namespace validation for secretRef", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongCACertificate]{
			{
				Name: "secretRef.namespace cannot be set (ReferenceGrant not yet supported)",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name:      "test-secret",
							Namespace: lo.ToPtr("other-namespace"),
						},
					},
				},
			},
			{
				Name: "valid: secretRef without namespace is allowed",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongCACertificate{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongCACertificate",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.KongCACertificateSpec{
				KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
					Cert: "cert",
				},
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, cfg, obj, common.NotSupportedByKIC, common.ControlPlaneRefRequired).
			RunWithConfig(t, cfg, scheme)
	})
}
