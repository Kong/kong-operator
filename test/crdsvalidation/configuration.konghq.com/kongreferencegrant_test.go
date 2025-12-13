package configuration_test

import (
	"testing"

	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
	"github.com/kong/kong-operator/test/envtest"
)

func TestKongReferenceGrant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sc := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, sc)

	t.Run("from field validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "valid from with KongCertificate",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
			},
			{
				Name: "valid from with KonnectGatewayControlPlane",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectAPIAuthConfiguration",
							},
						},
					},
				},
			},
			{
				Name: "from is required",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from: Required value"),
			},
			{
				Name: "from must have at least 1 item",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from in body should have at least 1 items"),
			},
			{
				Name: "from must have at most 16 items",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: func() []configurationv1alpha1.ReferenceGrantFrom {
							items := make([]configurationv1alpha1.ReferenceGrantFrom, 17)
							for i := range items {
								items[i] = configurationv1alpha1.ReferenceGrantFrom{
									Group:     "configuration.konghq.com",
									Kind:      "KongCertificate",
									Namespace: "test-namespace",
								}
							}
							return items
						}(),
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from: Too many: 17: must have at most 16 items"),
			},
			{
				Name: "from group is required",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from[0].group: Unsupported value"),
			},
			{
				Name: "from kind is required",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from[0].kind: Unsupported value"),
			},
			{
				Name: "from namespace is required",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongCertificate",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from[0].namespace in body should be at least 1 chars long"),
			},
			{
				Name: "from namespace cannot be empty",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from[0].namespace in body should be at least 1 chars long"),
			},
			{
				Name: "from invalid group/kind pair - configuration.konghq.com with wrong kind",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("pair group/kind is not allowed"),
			},
			{
				Name: "from invalid group/kind pair - konnect.konghq.com with wrong kind",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectAPIAuthConfiguration",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("pair group/kind is not allowed"),
			},
		}.
			RunWithConfig(t, cfg, sc)
	})

	t.Run("to field validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "valid to with Secret",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
			},
			{
				Name: "valid to with KonnectAPIAuthConfiguration",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectAPIAuthConfiguration",
							},
						},
					},
				},
			},
			{
				Name: "valid to with name specified",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			{
				Name: "to is required",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.to: Required value"),
			},
			{
				Name: "to must have at least 1 item",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.to in body should have at least 1 items"),
			},
			{
				Name: "to must have at most 16 items",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: func() []configurationv1alpha1.ReferenceGrantTo {
							items := make([]configurationv1alpha1.ReferenceGrantTo, 17)
							for i := range items {
								items[i] = configurationv1alpha1.ReferenceGrantTo{
									Group: "core",
									Kind:  "Secret",
								}
							}
							return items
						}(),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.to: Too many: 17: must have at most 16 items"),
			},
			{
				Name: "to group is required",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Kind: "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.to[0].group: Unsupported value"),
			},
			{
				Name: "to kind is required",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.to[0].kind: Unsupported value"),
			},
			{
				Name: "to invalid group/kind pair - core with wrong kind",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "KonnectAPIAuthConfiguration",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("pair group/kind is not allowed"),
			},
			{
				Name: "to invalid group/kind pair - konnect.konghq.com with wrong kind",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "Secret",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("pair group/kind is not allowed"),
			},
		}.
			RunWithConfig(t, cfg, sc)
	})

	t.Run("multiple from and to entries", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "multiple valid from entries",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "namespace-1",
							},
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "namespace-2",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
			},
			{
				Name: "multiple valid to entries",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "test-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("secret-1")),
							},
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("secret-2")),
							},
						},
					},
				},
			},
			{
				Name: "mixed valid from and to with different types",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-namespace",
							},
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "cp-namespace",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectAPIAuthConfiguration",
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, sc)
	})
}
