package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongKey(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "konnectNamespacedRef reference is valid",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
						},
					},
				},
			},
			{
				Name: "not providing konnectNamespacedRef when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
			},
			{
				Name: "not providing konnectID when type is konnectID yields an error",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectID,
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}")},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "konnectNamespacedRef reference name cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}")},
					},
					Status: configurationv1alpha1.KongKeyStatus{
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
				Update: func(ks *configurationv1alpha1.KongKey) {
					ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}")},
					},
					Status: configurationv1alpha1.KongKeyStatus{
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
				Update: func(ks *configurationv1alpha1.KongKey) {
					ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "konnectNamespaced reference cannot set namespace as it's not supported yet",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "test-konnect-control-plane",
								Namespace: "default",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}")},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource - it's not supported yet"),
			},
		}.Run(t)
	})

	t.Run("spec", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "KID must be set",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							JWK: lo.ToPtr("{}"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.kid in body should be at least 1 chars long"),
			},
			{
				Name: "one of JWK or PEM must be set",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Either 'jwk' or 'pem' must be set"),
			},
		}.Run(t)
	})

	t.Run("key set ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "when type is 'namespacedRef', namespacedRef is required",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefNamespacedRef,
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is namespacedRef, namespacedRef must be set"),
			},
			{
				Name: "'namespacedRef' type is accepted",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KeySetNamespacedRef{
								Name: "keyset-1",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
						},
					},
				},
			},
			{
				Name: "'konnectID' type is accepted",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type:      configurationv1alpha1.KeySetRefKonnectID,
							KonnectID: lo.ToPtr("keyset-1"),
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
						},
					},
				},
			},
			{
				Name: "when type is 'konnectID', konnectID is required",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefKonnectID,
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "unknown type is not accepted",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefType("unknown"),
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr(`Unsupported value: "unknown": supported values: "konnectID", "namespacedRef"`),
			},
		}.Run(t)
	})
}
