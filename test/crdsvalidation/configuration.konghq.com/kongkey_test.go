package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKongKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("jwk/cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongKey{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongKey",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.KongKeySpec{
				KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
					KID: "1",
					JWK: new("jwk"),
				},
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, cfg, obj, common.NotSupportedByKIC, common.ControlPlaneRefNotRequired).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("pem/cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongKey{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongKey",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
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

		common.NewCRDValidationTestCasesGroupCPRefChange(t, cfg, obj, common.NotSupportedByKIC, common.ControlPlaneRefNotRequired).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "KID must be set",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							JWK: new("{}"),
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: new("spec.kid in body should be at least 1 chars long"),
			},
			{
				Name: "one of JWK or PEM must be set",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("Either 'jwk' or 'pem' must be set"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("key set ref", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "when type is 'namespacedRef', namespacedRef is required",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefNamespacedRef,
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
						},
					},
				},
				ExpectedErrorMessage: new("when type is namespacedRef, namespacedRef must be set"),
			},
			{
				Name: "'namespacedRef' type is accepted",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
								Name: "keyset-1",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
						},
					},
				},
			},
			{
				Name: "'konnectID' type is accepted",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type:      configurationv1alpha1.KeySetRefKonnectID,
							KonnectID: new("keyset-1"),
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
						},
					},
				},
			},
			{
				Name: "when type is 'konnectID', konnectID is required",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefKonnectID,
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
						},
					},
				},
				ExpectedErrorMessage: new("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "unknown type is not accepted",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefType("unknown"),
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
						},
					},
				},
				ExpectedErrorMessage: new(`Unsupported value: "unknown": supported values: "konnectID", "namespacedRef"`),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKey]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
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
				ExpectedErrorMessage: new("spec.tags: Too many: 21: must have at most 20 items"),
			},
			{
				Name: "tags entries must not be longer than 128 characters",
				TestObject: &configurationv1alpha1.KongKey{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongKeySpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "1",
							JWK: new("{}"),
							Tags: []string{
								lo.RandomString(129, lo.AlphanumericCharset),
							},
						},
					},
				},
				ExpectedErrorMessage: new("tags entries must not be longer than 128 characters"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
