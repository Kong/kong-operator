package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongService(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongService]{
			{
				Name: "konnectNamespacedRef reference is valid",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
			},
			{
				Name: "not providing konnectNamespacedRef when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
			},
			{
				Name: "not providing konnectID when type is konnectID yields an error",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectID,
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "providing konnectID when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectID: lo.ToPtr("123456"),
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
			},

			{
				Name: "providing konnectNamespacedRef when type is konnectID yields an error",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectID,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "providing konnectNamespacedRef and konnectID when type is konnectID yields an error",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
							KonnectID: lo.ToPtr("123456"),
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectNamespacedRef must not be set"),
			},
			{
				Name: "providing konnectID and konnectNamespacedRef when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectID: lo.ToPtr("123456"),
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectID must not be set"),
			},
			{
				Name: "providing namespace in konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "test-konnect-control-plane",
								Namespace: "another-namespace",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
			},
			{
				Name: "konnectNamespacedRef reference name cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
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
				Update: func(ks *configurationv1alpha1.KongService) {
					ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "example.com",
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
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
				Update: func(ks *configurationv1alpha1.KongService) {
					ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongService]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
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
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
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
				TestObject: &configurationv1alpha1.KongService{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
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
}
