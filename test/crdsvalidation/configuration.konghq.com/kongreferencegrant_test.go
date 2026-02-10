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
				Name: "without a name works",
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
								Kind:      "KongRoute",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-cp")),
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongRoute to KongService reference", func(t *testing.T) {
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
								Kind:      "KongRoute",
								Group:     "configuration.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongService",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-service")),
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("GatewayConfiguration to KonnectAPIAuthConfiguration reference", func(t *testing.T) {
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
								Kind:      "GatewayConfiguration",
								Group:     "gateway-operator.konghq.com",
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
				Name: "with a name works",
				TestObject: &configurationv1alpha1.KongReferenceGrant{
					TypeMeta:   typeMeta,
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Namespace: configurationv1alpha1.Namespace("other"),
								Kind:      "GatewayConfiguration",
								Group:     "gateway-operator.konghq.com",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectAPIAuthConfiguration",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-auth")),
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	toKonnectCP := configurationv1alpha1.ReferenceGrantTo{
		Group: "konnect.konghq.com",
		Kind:  "KonnectGatewayControlPlane",
	}

	toKongPlugin := configurationv1alpha1.ReferenceGrantTo{
		Group: "configuration.konghq.com",
		Kind:  "KongPlugin",
	}
	toKongService := configurationv1alpha1.ReferenceGrantTo{
		Group: "configuration.konghq.com",
		Kind:  "KongService",
	}

	t.Run("KongVault to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "", "KongVault", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "", "KongVault", toKonnectCP),
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
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongKey", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongKey", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongKeySet to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongKeySet", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongKeySet", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongUpstream to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongUpstream", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongUpstream", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongCertificate to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongCertificate", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongCertificate", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongCACertificate to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongCACertificate", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongCACertificate", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongService to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongService", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongService", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongConsumer to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongConsumer", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongConsumer", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongConsumerGroup to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongConsumerGroup", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongConsumerGroup", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongPluginBinding to KongPlugin", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongPluginBinding", toKongPlugin),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongPluginBinding", toKongPlugin),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongRoute to KonnectGatewayControlPlane", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongRoute", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongRoute", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongRoute to KongService", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "KongRoute", toKongService),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "KongRoute", toKongService),
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
	to configurationv1alpha1.ReferenceGrantTo,
) common.TestCase[*configurationv1alpha1.KongReferenceGrant] {
	from := []configurationv1alpha1.ReferenceGrantFrom{
		{
			Namespace: configurationv1alpha1.Namespace(fromNamespace),
			Kind:      configurationv1alpha1.Kind(fromKind),
			Group:     "configuration.konghq.com",
		},
	}

	toWithName := to
	toWithName.Name = lo.ToPtr(configurationv1alpha1.ObjectName("my-object"))

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
						to,
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
						toWithName,
					},
				},
			},
		}
	default:
		panic("should not happen")
	}
}
