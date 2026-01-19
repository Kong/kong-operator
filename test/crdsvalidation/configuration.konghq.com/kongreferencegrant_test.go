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
				ExpectedErrorMessage: lo.ToPtr("Only KongConsumerGroup, KongCertificate, KongCACertificate, KongDataPlaneClientCertificate, KongService, KongUpstream, KongKey, KongKeySet, and KongVault kinds are supported for 'configuration.konghq.com' group"),
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
				ExpectedErrorMessage: lo.ToPtr("Only KongConsumerGroup, KongCertificate, KongCACertificate, KongDataPlaneClientCertificate, KongService, KongUpstream, KongKey, KongKeySet, and KongVault kinds are supported for 'configuration.konghq.com' group"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongVault to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "", "KongVault"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "", "KongVault"),
			{
				Name: "non-empty namespace is rejected",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("default"),
								Kind:      "KongVault",
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
				ExpectedErrorMessage: lo.ToPtr("namespace must be empty for KongVault and non-empty for other kinds"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongKey to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongKey"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongKey"),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongKeySet to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongKeySet"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongKeySet"),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongUpstream to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongUpstream"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongUpstream"),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongCertificate to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongCertificate"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongCertificate"),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongCACertificate to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongCACertificate"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongCACertificate"),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongService to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongService"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongService"),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongConsumerGroup to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongConsumerGroup"),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongConsumerGroup"),
		}.RunWithConfig(t, cfg, scheme)
	})
}

type typeOfKongReferenceGrantCase byte

const (
	withoutName typeOfKongReferenceGrantCase = iota
	withName
)

func kongReferenceGrantCase(
	typ typeOfKongReferenceGrantCase,
	typeMeta metav1.TypeMeta,
	ns string,
	fromNamespace string,
	fromKind string,
) common.TestCase[*configurationv1alpha1.KongReferenceGrant] {
	from := []configurationv1alpha1.ReferenceGrantFrom{
		{
			Namespace: configurationv1alpha1.Namespace(fromNamespace),
			Kind:      configurationv1alpha1.Kind(fromKind),
			Group:     "configuration.konghq.com",
		},
	}

	switch typ {
	case withoutName:
		return common.TestCase[*configurationv1alpha1.KongReferenceGrant]{
			Name: "without a name works",
			TestObject: &configurationv1alpha1.KongReferenceGrant{
				TypeMeta:   typeMeta,
				ObjectMeta: common.CommonObjectMeta(ns),
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: from,
					To: []configurationv1alpha1.ReferenceGrantTo{
						{
							Group: "konnect.konghq.com",
							Kind:  "KonnectGatewayControlPlane",
						},
					},
				},
			},
		}
	case withName:
		return common.TestCase[*configurationv1alpha1.KongReferenceGrant]{
			Name: "with a name works",
			TestObject: &configurationv1alpha1.KongReferenceGrant{
				TypeMeta:   typeMeta,
				ObjectMeta: common.CommonObjectMeta(ns),
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: from,
					To: []configurationv1alpha1.ReferenceGrantTo{
						{
							Group: "konnect.konghq.com",
							Kind:  "KonnectGatewayControlPlane",
							Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-control-plane")),
						},
					},
				},
			},
		}
	default:
		panic("should not happen")
	}
}
