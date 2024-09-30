package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var cpRef = testCasesGroup{
	Name: "controlPlaneRef",
	TestCases: []testCase{
		{
			Name: "konnectNamespacedRef reference is valid",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
		},
		{
			Name: "not providing konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					},
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		},
		{
			Name: "not providing konnectID when type is konnectID yields an error",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectID,
					},
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		},
		{
			Name: "konnectNamespacedRef reference name cannot be changed when an entity is Programmed",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			KongDataplaneCertificateStatus: &configurationv1alpha1.KongDataplaneCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongDataplaneCertificate) {
				ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			KongDataplaneCertificateStatus: &configurationv1alpha1.KongDataplaneCertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(ks *configurationv1alpha1.KongDataplaneCertificate) {
				ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
		{
			Name: "konnectNamespaced reference cannot set namespace as it's not supported yet",
			KongDataplaneCertificate: configurationv1alpha1.KongDataplaneCertificate{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongDataplaneCertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name:      "test-konnect-control-plane",
							Namespace: "default",
						},
					},
					KongDataplaneCertificateAPISpec: configurationv1alpha1.KongDataplaneCertificateAPISpec{
						Cert: "cert",
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource - it's not supported yet"),
		},
	},
}
