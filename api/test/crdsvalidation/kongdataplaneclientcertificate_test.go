package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

func TestKongDataPlaneClientCertificate(t *testing.T) {
	obj := &configurationv1alpha1.KongDataPlaneClientCertificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongDataPlaneClientCertificate",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: commonObjectMeta,
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
				Cert: "cert",
			},
		},
	}

	t.Run("cp ref", func(t *testing.T) {
		NewCRDValidationTestCasesGroupCPRefChange(t, obj, NotSupportedByKIC).Run(t)
	})

	t.Run("cp ref, type=kic", func(t *testing.T) {
		NewCRDValidationTestCasesGroupCPRefChangeKICUnsupportedTypes(t, obj, EmptyControlPlaneRefNotAllowed).Run(t)
	})

	t.Run("spec", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongDataPlaneClientCertificate]{
			{
				Name: "valid KongDataPlaneClientCertificate",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
			},
			{
				Name: "cert is required",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: commonObjectMeta,
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert in body should be at least 1 chars long"),
			},
			{
				Name: "cert can be altered before programmed",
				TestObject: &configurationv1alpha1.KongDataPlaneClientCertificate{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
						KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
							Cert: "cert",
						},
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
		}.Run(t)
	})
}
