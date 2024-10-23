package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongCertificate(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongCertificate]{
			{
				Name: "konnectNamespacedRef reference is valid",
				TestObject: &configurationv1alpha1.KongCertificate{
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
				TestObject: &configurationv1alpha1.KongCertificate{
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
				TestObject: &configurationv1alpha1.KongCertificate{
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
				TestObject: &configurationv1alpha1.KongCertificate{
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
					Status: configurationv1alpha1.KongCertificateStatus{
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
				Update: func(ks *configurationv1alpha1.KongCertificate) {
					ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongCertificate{
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
					Status: configurationv1alpha1.KongCertificateStatus{
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
				Update: func(ks *configurationv1alpha1.KongCertificate) {
					ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
		}.Run(t)
	})

	t.Run("required fields", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongCertificate]{
			{
				Name: "cert field is required",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Key: "test-key",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.cert: Required value"),
			},
			{
				Name: "key field is required",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
							Cert: "test-cert",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.key: Required value"),
			},
			{
				Name: "cert and key fields are required",
				TestObject: &configurationv1alpha1.KongCertificate{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongCertificateSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
		}.Run(t)

		t.Run("tags validation", func(t *testing.T) {
			CRDValidationTestCasesGroup[*configurationv1alpha1.KongCertificate]{
				{
					Name: "up to 20 tags are allowed",
					TestObject: &configurationv1alpha1.KongCertificate{
						ObjectMeta: commonObjectMeta,
						Spec: configurationv1alpha1.KongCertificateSpec{
							ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
						ObjectMeta: commonObjectMeta,
						Spec: configurationv1alpha1.KongCertificateSpec{
							ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
						ObjectMeta: commonObjectMeta,
						Spec: configurationv1alpha1.KongCertificateSpec{
							ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
			}.Run(t)
		})
	})
}
