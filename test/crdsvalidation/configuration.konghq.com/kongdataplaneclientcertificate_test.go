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

func TestKongDataPlaneClientCertificate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	obj := &configurationv1alpha1.KongDataPlaneClientCertificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongDataPlaneClientCertificate",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: common.CommonObjectMeta(ns.Name),
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
				Cert: "cert",
			},
		},
	}

	t.Run("cp ref", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupCPRefChange(t, cfg, obj, common.NotSupportedByKIC, common.ControlPlaneRefRequired).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("cp ref, type=kic", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupCPRefChangeKICUnsupportedTypes(t, obj, common.EmptyControlPlaneRefNotAllowed).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongDataPlaneClientCertificate]{
			{
				Name: "valid KongDataPlaneClientCertificate",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
			},
			{
				Name: "cert is required",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert in body should be at least 1 chars long"),
			},
			{
				Name: "cert can be altered before programmed",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					Status: configurationv1alpha1.KongDataPlaneClientCertificateStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionFalse,
								Reason:             "Pending",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(k *configurationv1alpha1.KongDataPlaneClientCertificate) {
					k.Spec.Cert = "cert2"
				},
			},
			{
				Name: "cert becomes immutable after programmed",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
					Status: configurationv1alpha1.KongDataPlaneClientCertificateStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionTrue,
								Reason:             "Programmed",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(k *configurationv1alpha1.KongDataPlaneClientCertificate) {
					k.Spec.Cert = "cert2"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.cert is immutable when an entity is already Programmed"),
			},
			{
				Name: "Can adopt in match mode",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "test-dp-cert",
							},
						},
					},
				},
			},
			{
				Name: "Cannot adopt in override mode",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "test-dp-cert",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Only 'match' mode adoption is supported"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
