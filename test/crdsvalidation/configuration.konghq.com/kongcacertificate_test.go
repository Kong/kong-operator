package configuration_test

import (
	"testing"

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
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeInline),
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
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeInline),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{},
					},
				},
				ExpectedErrorMessage: new("spec.cert is required when type is 'inline'"),
			},
			{
				Name: "type=inline with empty cert returns error",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeInline),
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
				ExpectedErrorMessage: new("spec.cert is required when type is 'inline'"),
			},
			{
				Name: "type=secretRef requires secretRef field",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
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
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: new("spec.secretRef is required when type is 'secretRef'"),
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
				ExpectedErrorMessage: new("cert and secretRef cannot be set at the same time"),
			},
			{
				Name: "valid: secretRef alone is allowed",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
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
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeInline),
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
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name:      "test-secret",
							Namespace: new("other-namespace"),
						},
					},
				},
			},
			{
				Name: "valid: secretRef without namespace is allowed",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongCACertificateSpec{
						Type: new(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
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
			Kind:       "KongCACertificate",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
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

	t.Run("spec.id mutability", func(t *testing.T) {
		programmedTrue := metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionTrue,
			Reason:             "Valid",
			LastTransitionTime: metav1.Now(),
		}

		existingID := "11111111-1111-1111-1111-111111111111"
		newID := "22222222-2222-2222-2222-222222222222"

		baseSpec := func(id *string) configurationv1alpha1.KongCACertificateSpec {
			return configurationv1alpha1.KongCACertificateSpec{
				ID: id,
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name: "test-konnect-control-plane",
					},
				},
				Cert: "test-cert",
			}
		}

		common.TestCasesGroup[*configurationv1alpha1.KongCACertificate]{
			{
				Name: "spec.id can be set before Programmed",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       baseSpec(nil),
				},
				Update: func(obj *configurationv1alpha1.KongCACertificate) {
					obj.Spec.ID = &existingID
				},
			},
			{
				Name: "spec.id can be changed before Programmed",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       baseSpec(&existingID),
				},
				Update: func(obj *configurationv1alpha1.KongCACertificate) {
					obj.Spec.ID = &newID
				},
			},
			{
				Name: "spec.id cannot be set after Programmed=True",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       baseSpec(nil),
					Status: configurationv1alpha1.KongCACertificateStatus{
						Conditions: []metav1.Condition{programmedTrue},
					},
				},
				Update: func(obj *configurationv1alpha1.KongCACertificate) {
					obj.Spec.ID = &existingID
				},
				ExpectedUpdateErrorMessage: new("spec.id is immutable when an entity is already Programmed"),
			},
			{
				Name: "spec.id cannot be changed after Programmed=True",
				TestObject: &configurationv1alpha1.KongCACertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       baseSpec(&existingID),
					Status: configurationv1alpha1.KongCACertificateStatus{
						Conditions: []metav1.Condition{programmedTrue},
					},
				},
				Update: func(obj *configurationv1alpha1.KongCACertificate) {
					obj.Spec.ID = &newID
				},
				ExpectedUpdateErrorMessage: new("spec.id is immutable when an entity is already Programmed"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
