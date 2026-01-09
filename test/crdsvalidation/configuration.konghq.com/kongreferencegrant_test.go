package configuration_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
	"github.com/kong/kong-operator/test/envtest"
)

func TestKongReferenceGrant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	typeMeta := metav1.TypeMeta{
		Kind:       "KongReferenceGrant",
		APIVersion: configurationv1alpha1.GroupVersion.String(),
	}

	t.Run("KongCertificate to Secret reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "without a name works",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongCertificate",
								Group:     "configuration.konghq.com",
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
				Name: "with a name works",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongCertificate",
								Group:     "configuration.konghq.com",
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
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongService to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "without a name works",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongService",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
			},
			{
				Name: "with a name works",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongService",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-control-plane")),
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongRoute to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "is not supported",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongRoute",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Only KongCertificate, KongCACertificate, KongService, and KongUpstream kinds are supported for 'configuration.konghq.com' group"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongRoute to KongService reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "is not supported",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongRoute",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongService",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Only KongCertificate, KongCACertificate, KongService, and KongUpstream kinds are supported for 'configuration.konghq.com' group"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongUpstream to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			{
				Name: "without a name works",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongUpstream",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
			},
			{
				Name: "with a name works",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "KongUpstream",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-control-plane")),
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
