package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKongVault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("cp ref", func(t *testing.T) {
		obj := &configurationv1alpha1.KongVault{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongVault",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.KongVaultSpec{
				Backend: "aws",
				Prefix:  "aws-vault",
			},
		}

		common.NewCRDValidationTestCasesGroupCPRefChange(t, cfg, obj, common.SupportedByKIC, common.ControlPlaneRefNotRequired).
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongVault]{
			{
				Name: "backend must be non-empty",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Prefix: "aws-vault",
					},
				},
				ExpectedErrorMessage: new("spec.backend: Invalid value"),
			},
			{
				Name: "prefix must be non-empty",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
					},
				},
				ExpectedErrorMessage: new("spec.prefix: Invalid value"),
			},
			{
				Name: "prefix is immutatble",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault",
					},
				},
				Update: func(v *configurationv1alpha1.KongVault) {
					v.Spec.Prefix += "-1"
				},
				ExpectedUpdateErrorMessage: new("The spec.prefix field is immutable"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("configStoreRef", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongVault]{
			{
				Name: "configStoreRef is allowed with the konnect backend",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "konnect",
						Prefix:  "certvault",
						ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
							Name:      "tls-cert-keys",
							Namespace: ns.Name,
						},
					},
				},
			},
			{
				Name: "configStoreRef is not allowed with a non-konnect backend",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault",
						ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
							Name:      "tls-cert-keys",
							Namespace: ns.Name,
						},
					},
				},
				ExpectedErrorMessage: new("spec.configStoreRef is only supported when spec.backend is 'konnect'"),
			},
			{
				Name: "configStoreRef requires a name",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "konnect",
						Prefix:  "certvault",
						ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
							Namespace: ns.Name,
						},
					},
				},
				ExpectedErrorMessage: new("spec.configStoreRef.name: Invalid value"),
			},
			{
				Name: "configStoreRef requires a namespace as KongVault is cluster scoped",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "konnect",
						Prefix:  "certvault",
						ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
							Name: "tls-cert-keys",
						},
					},
				},
				ExpectedErrorMessage: new("spec.configStoreRef.namespace: Invalid value"),
			},
			{
				Name: "configStoreRef kind defaults to KonnectConfigStore",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "konnect",
						Prefix:  "certvault",
						ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
							Name:      "tls-cert-keys",
							Namespace: ns.Name,
						},
					},
				},
				Assert: func(t *testing.T, v *configurationv1alpha1.KongVault) {
					require.Equal(t, configurationv1alpha1.KonnectConfigStoreKind, v.Spec.ConfigStoreRef.Kind)
				},
			},
			{
				Name: "configStoreRef rejects other kinds",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "konnect",
						Prefix:  "certvault",
						ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
							Kind:      "Secret",
							Name:      "tls-cert-keys",
							Namespace: ns.Name,
						},
					},
				},
				ExpectedErrorMessage: new("spec.configStoreRef.kind: Unsupported value"),
			},
			{
				Name: "configStoreRef can be added to an existing konnect vault",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "konnect",
						Prefix:  "certvault",
					},
				},
				Update: func(v *configurationv1alpha1.KongVault) {
					v.Spec.ConfigStoreRef = &configurationv1alpha1.KonnectConfigStoreRef{
						Name:      "tls-cert-keys",
						Namespace: ns.Name,
					}
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongVault]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Backend: "aws",
						Prefix:  "aws-vault",
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
			{
				Name: "more than 20 tags are not allowed",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Backend: "aws",
						Prefix:  "aws-vault",
						Tags: func() []string {
							var tags []string
							for i := range 21 {
								tags = append(tags, fmt.Sprintf("tag-%d", i))
							}
							return tags
						}(),
					},
				},
				ExpectedErrorMessage: new("spec.tags: Too many: 21: must have at most 20 items"),
			},
			{
				Name: "tags entries must not be longer than 128 characters",
				TestObject: &configurationv1alpha1.KongVault{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongVaultSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						Backend: "aws",
						Prefix:  "aws-vault",
						Tags: []string{
							lo.RandomString(129, lo.AlphanumericCharset),
						},
					},
				},
				ExpectedErrorMessage: new("tags entries must not be longer than 128 characters"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
