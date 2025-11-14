package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
	"github.com/kong/kong-operator/test/envtest"
)

func TestKongKeySet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, _ := envtest.Setup(t, ctx, scheme)

	obj := &configurationv1alpha1.KongKeySet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongKeySet",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: common.CommonObjectMeta,
		Spec: configurationv1alpha1.KongKeySetSpec{
			KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
				Name: "keyset",
			},
		},
	}

	t.Run("cp ref", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupCPRefChange(t, obj, common.NotSupportedByKIC, common.ControlPlaneRefRequired).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("cp ref, type=kic", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupCPRefChangeKICUnsupportedTypes(t, obj, common.EmptyControlPlaneRefNotAllowed).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKeySet]{
			{
				Name: "name must be set",
				TestObject: &configurationv1alpha1.KongKeySet{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySetSpec{
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.name in body should be at least 1 chars long"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongKeySet]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongKeySet{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySetSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "keyset",
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
				TestObject: &configurationv1alpha1.KongKeySet{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySetSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "keyset",
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
				TestObject: &configurationv1alpha1.KongKeySet{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongKeySetSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "keyset",
							Tags: []string{
								lo.RandomString(129, lo.AlphanumericCharset),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("tags entries must not be longer than 128 characters"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
