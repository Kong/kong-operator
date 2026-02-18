package configuration_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
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
								Name:  new(configurationv1alpha1.ObjectName("my-secret")),
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
								Name:  new(configurationv1alpha1.ObjectName("my-cp")),
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
								Name:  new(configurationv1alpha1.ObjectName("my-service")),
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("GatewayConfiguration to KonnectAPIAuthConfiguration reference", func(t *testing.T) {
		toKonnectAPIAuth := configurationv1alpha1.ReferenceGrantTo{
			Group: "konnect.konghq.com",
			Kind:  "KonnectAPIAuthConfiguration",
		}
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "gateway-operator.konghq.com", "GatewayConfiguration", toKonnectAPIAuth),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "gateway-operator.konghq.com", "GatewayConfiguration", toKonnectAPIAuth),
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
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "", "configuration.konghq.com", "KongVault", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "", "configuration.konghq.com", "KongVault", toKonnectCP),
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
				ExpectedErrorMessage: new("namespace must be empty for KongVault and non-empty for other kinds"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongKey to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongKey", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongKey", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongKeySet to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongKeySet", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongKeySet", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongUpstream to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongUpstream", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongUpstream", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongCertificate to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongCertificate", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongCertificate", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongCACertificate to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongCACertificate", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongCACertificate", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongService to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongService", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongService", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongConsumer to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongConsumer", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongConsumer", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongConsumerGroup to KonnectGatewayControlPlane reference", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongConsumerGroup", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongConsumerGroup", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongPluginBinding to KongPlugin", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongPluginBinding", toKongPlugin),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongPluginBinding", toKongPlugin),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongRoute to KonnectGatewayControlPlane", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongRoute", toKonnectCP),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongRoute", toKonnectCP),
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("KongRoute to KongService", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongReferenceGrant]{
			kongReferenceGrantCase(withName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongRoute", toKongService),
			kongReferenceGrantCase(withoutName, typeMeta, ns.Name, "other", "configuration.konghq.com", "KongRoute", toKongService),
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
	fromGroup string,
	fromKind string,
	to configurationv1alpha1.ReferenceGrantTo,
) common.TestCase[*configurationv1alpha1.KongReferenceGrant] {
	from := []configurationv1alpha1.ReferenceGrantFrom{
		{
			Namespace: configurationv1alpha1.Namespace(fromNamespace),
			Kind:      configurationv1alpha1.Kind(fromKind),
			Group:     configurationv1alpha1.Group(fromGroup),
		},
	}

	toWithName := to
	toWithName.Name = new(configurationv1alpha1.ObjectName("my-object"))

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
