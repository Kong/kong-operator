package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
)

func TestKongKey(t *testing.T) {
	t.Run("jwk/cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongKey{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongKey",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta,
			Spec: configurationv1alpha1.KongKeySpec{
				KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
					KID: "1",
					JWK: lo.ToPtr("jwk"),
				},
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, obj, common.NotSupportedByKIC, common.ControlPlaneRefNotRequired).Run(t)
	})

	t.Run("pem/cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongKey{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongKey",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta,
			Spec: configurationv1alpha1.KongKeySpec{
				KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
					KID: "1",
					PEM: &configurationv1alpha1.PEMKeyPair{
						PublicKey:  "public",
						PrivateKey: "private",
					},
				},
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, obj, common.NotSupportedByKIC, common.ControlPlaneRefNotRequired).Run(t)
	})

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "KID must be set",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							JWK: lo.ToPtr("{}"),
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.kid in body should be at least 1 chars long"),
			},
			{
				Name: "one of JWK or PEM must be set",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Either 'jwk' or 'pem' must be set"),
			},
		}.Run(t)
	})

	t.Run("key set ref", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "when type is 'namespacedRef', namespacedRef is required",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta,
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
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
					ObjectMeta: common.CommonObjectMeta,
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
					ObjectMeta: common.CommonObjectMeta,
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
					ObjectMeta: common.CommonObjectMeta,
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

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
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
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
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
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: lo.ToPtr("{}"),
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
