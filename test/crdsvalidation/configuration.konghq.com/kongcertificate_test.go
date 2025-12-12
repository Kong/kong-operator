package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
	"github.com/kong/kong-operator/test/envtest"
)

func TestKongCertificate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongCertificate{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongCertificate",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.KongCertificateSpec{
				KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
					Cert: "test-cert",
					Key:  "test-key",
				},
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, cfg, obj, common.NotSupportedByKIC, common.ControlPlaneRefRequired).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("required fields", func(t *testing.T) {
		t.Run("tags validation", func(t *testing.T) {
			common.TestCasesGroup[*configurationv1alpha1.KongCertificate]{
				{
					Name: "up to 20 tags are allowed",
					TestObject: &configurationv1alpha1.KongCertificate{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: configurationv1alpha1.KongCertificateSpec{
							ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
									Name: "test-konnect-control-plane",
								},
							},
							KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
								Key:  "key",
								Cert: "cert",
								Tags: func() []string {
									var tags []string
									for i := range 20 {
										tags = append(tags, fmt.Sprintf("tag-%d", i))
									}
									return tags
								}(),
							},
						},
					},
				},
				{
					Name: "more than 20 tags are not allowed",
					TestObject: &configurationv1alpha1.KongCertificate{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: configurationv1alpha1.KongCertificateSpec{
							ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
									Name: "test-konnect-control-plane",
								},
							},
							KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
								Key:  "key",
								Cert: "cert",
								Tags: func() []string {
									var tags []string
									for i := range 21 {
										tags = append(tags, fmt.Sprintf("tag-%d", i))
									}
									return tags
								}(),
							},
						},
					},
					ExpectedErrorMessage: lo.ToPtr("spec.tags: Too many: 21: must have at most 20 items"),
				},
				{
					Name: "tags entries must not be longer than 128 characters",
					TestObject: &configurationv1alpha1.KongCertificate{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: configurationv1alpha1.KongCertificateSpec{
							ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
									Name: "test-konnect-control-plane",
								},
							},
							KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
								Key:  "key",
								Cert: "cert",
								Tags: []string{
									lo.RandomString(129, lo.AlphanumericCharset),
								},
							},
						},
					},
					ExpectedErrorMessage: lo.ToPtr("tags entries must not be longer than 128 characters"),
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})
	})

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongCertificate]{
			{
				Name: "type=inline requires cert and key fields",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
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
			{
				Name: "type=inline with missing cert returns error",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Key: "test-key",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert is required when type is 'inline'"),
			},
			{
				Name: "type=inline with empty cert returns error",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Cert: "",
							Key:  "test-key",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert is required when type is 'inline'"),
			},
			{
				Name: "type=inline with missing key returns error",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Cert: "test-cert",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.key is required when type is 'inline'"),
			},
			{
				Name: "type=secretRef requires secretRef field",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
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
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
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
		common.TestCasesGroup[*configurationv1alpha1.KongCertificate]{
			{
				Name: "cert/key cannot be mixed with secretRef",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Cert: "test-cert",
							Key:  "test-key",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cert/key and secretRef/secretRefAlt cannot be set at the same time"),
			},
			{
				Name: "cert cannot be mixed with secretRefAlt",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRefAlt: &commonv1alpha1.NamespacedRef{
							Name: "test-secret-alt",
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Cert: "test-cert",
							Key:  "test-key",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cert/key and secretRef/secretRefAlt cannot be set at the same time"),
			},
			{
				Name: "key alone cannot be mixed with secretRef",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Key: "test-key",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cert/key and secretRef/secretRefAlt cannot be set at the same time"),
			},
			{
				Name: "certAlt/keyAlt cannot be mixed with secretRef",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							CertAlt: "test-cert-alt",
							KeyAlt:  "test-key-alt",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cert_alt/key_alt and secretRef/secretRefAlt cannot be set at the same time"),
			},
			{
				Name: "certAlt alone cannot be mixed with secretRefAlt",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRefAlt: &commonv1alpha1.NamespacedRef{
							Name: "test-secret-alt",
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							CertAlt: "test-cert-alt",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cert_alt/key_alt and secretRef/secretRefAlt cannot be set at the same time"),
			},
			{
				Name: "valid: secretRef and secretRefAlt can be used together",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
						SecretRefAlt: &commonv1alpha1.NamespacedRef{
							Name: "test-secret-alt",
						},
					},
				},
			},
			{
				Name: "valid: cert/key and certAlt/keyAlt can be used together for inline",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Cert:    "test-cert",
							Key:     "test-key",
							CertAlt: "test-cert-alt",
							KeyAlt:  "test-key-alt",
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("namespace validation for secretRef", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongCertificate]{
			{
				Name: "secretRef.namespace set",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
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
				Name: "secretRefAlt.namespace set",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
						SecretRefAlt: &commonv1alpha1.NamespacedRef{
							Name:      "test-secret-alt",
							Namespace: lo.ToPtr("other-namespace"),
						},
					},
				},
			},
			{
				Name: "valid: secretRef without namespace is allowed",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
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
				Name: "valid: both secretRef and secretRefAlt without namespace are allowed",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "test-secret",
						},
						SecretRefAlt: &commonv1alpha1.NamespacedRef{
							Name: "test-secret-alt",
						},
					},
				},
			},
			{
				Name: "valid: both secretRef and secretRefAlt with namespace are allowed",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCertificateSpec{
						Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
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
						SecretRefAlt: &commonv1alpha1.NamespacedRef{
							Name:      "test-secret-alt",
							Namespace: lo.ToPtr("other-namespace"),
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
