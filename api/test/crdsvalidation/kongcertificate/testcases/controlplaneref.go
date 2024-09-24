package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var cpRef = testCasesGroup{
	Name: "cp ref validation",
	TestCases: []testCase{
		{
			Name: "konnectNamespacedRef reference is valid",
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert",
						Key:  "key",
					},
				},
			},
		},
		{
			Name: "not providing konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert",
						Key:  "key",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		},
		{
			Name: "not providing konnectID when type is konnectID yields an error",
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectID,
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert",
						Key:  "key",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		},
		{
			Name: "konnectNamespacedRef reference name cannot be changed when an entity is Programmed",
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert",
						Key:  "key",
					},
				},
			},
			KongCertificateStatus: &configurationv1alpha1.KongCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongCertificate) {
				ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
			KongCertificate: configurationv1alpha1.KongCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert",
						Key:  "key",
					},
				},
			},
			KongCertificateStatus: &configurationv1alpha1.KongCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongCertificate) {
				ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
	},
}
