package policychain

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestClaimValue(t *testing.T) {
	t.Parallel()
	chain := func(namespace *string) *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain {
		return &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
			Namespace: "chains", Name: "chain", UID: "uid",
			Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainSpec{
				EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "vc", Namespace: namespace},
				},
			},
		}
	}
	for name, tc := range map[string]struct {
		namespace *string
		wantKey   client.ObjectKey
		wantClaim string
	}{
		"a virtual cluster of the namespace of the chain": {
			wantKey: client.ObjectKey{Namespace: "chains", Name: "vc"}, wantClaim: "chain/uid",
		},
		"the namespace of the chain set explicitly": {
			namespace: new("chains"), wantKey: client.ObjectKey{Namespace: "chains", Name: "vc"}, wantClaim: "chain/uid",
		},
		"a virtual cluster of another namespace": {
			namespace: new("clusters"), wantKey: client.ObjectKey{Namespace: "clusters", Name: "vc"}, wantClaim: "chains/chain/uid",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := chain(tc.namespace)
			key, ok := ParentKey(c)
			require.True(t, ok)
			require.Equal(t, tc.wantKey, key)
			require.Equal(t, tc.wantClaim, ClaimValue(c))
			owner, uid, ok := ParseClaim(ClaimValue(c), key.Namespace)
			require.True(t, ok)
			require.Equal(t, client.ObjectKeyFromObject(c), owner)
			require.Equal(t, "uid", uid)
		})
	}

	t.Run("no virtual cluster referenced by name", func(t *testing.T) {
		t.Parallel()
		c := chain(nil)
		c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = ""
		_, ok := ParentKey(c)
		require.False(t, ok)
	})
}

func TestParseClaim(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		claim   string
		wantKey client.ObjectKey
		wantUID string
		wantOK  bool
	}{
		{claim: "chain/uid", wantKey: client.ObjectKey{Namespace: "parent", Name: "chain"}, wantUID: "uid", wantOK: true},
		{claim: "other/chain/uid", wantKey: client.ObjectKey{Namespace: "other", Name: "chain"}, wantUID: "uid", wantOK: true},
		{claim: "chain"},
		{claim: "chain/"},
		{claim: "/uid"},
		{claim: "other//uid"},
		{claim: "a/b/c/d"},
	} {
		key, uid, ok := ParseClaim(tc.claim, "parent")
		require.Equal(t, tc.wantOK, ok, tc.claim)
		require.Equal(t, tc.wantKey, key, tc.claim)
		require.Equal(t, tc.wantUID, uid, tc.claim)
	}
}

func TestHolder(t *testing.T) {
	t.Parallel()
	parent := client.ObjectKey{Namespace: "clusters", Name: "vc"}
	chain := func(namespace string) *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain {
		return &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
			Namespace: namespace, Name: "chain", UID: "uid",
			Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainSpec{
				EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: parent.Name, Namespace: new(parent.Namespace)},
				},
			},
		}
	}
	// The chain of the claim, of the namespace of the cluster, now
	// referencing another one.
	otherParent := chain(parent.Namespace)
	otherParent.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "other"
	// A listener policy chain may only claim a listener of its namespace.
	listenerChain := func(namespace string) *configurationv1alpha1.EventGatewayListenerPolicyChain {
		return &configurationv1alpha1.EventGatewayListenerPolicyChain{
			Namespace: namespace, Name: "chain", UID: "uid",
			Spec: configurationv1alpha1.EventGatewayListenerPolicyChainSpec{
				EventGatewayListenerRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: parent.Name, Namespace: new(parent.Namespace)},
				},
			},
		}
	}
	grant := &configurationv1alpha1.KongReferenceGrant{
		Name: "chains", Namespace: parent.Namespace,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayVirtualClusterPolicyChain", Namespace: "chains"}},
			To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "configuration.konghq.com", Kind: "EventGatewayVirtualCluster"}},
		},
	}
	for name, tc := range map[string]struct {
		objs     []client.Object
		claim    string
		holds    bool
		listener bool
	}{
		"a chain of the namespace of the cluster":        {objs: []client.Object{chain(parent.Namespace)}, claim: "chain/uid", holds: true},
		"a granted chain of another namespace":           {objs: []client.Object{chain("chains"), grant.DeepCopy()}, claim: "chains/chain/uid", holds: true},
		"a chain of another namespace no longer granted": {objs: []client.Object{chain("chains")}, claim: "chains/chain/uid"},
		"another incarnation":                            {objs: []client.Object{chain(parent.Namespace)}, claim: "chain/other-uid"},
		"a chain that is gone":                           {claim: "chain/uid"},
		"a malformed claim":                              {claim: "chain"},
		"a chain referencing another virtual cluster":    {objs: []client.Object{otherParent}, claim: "chain/uid"},
		"a listener chain of another namespace":          {listener: true, objs: []client.Object{listenerChain("chains"), grant.DeepCopy()}, claim: "chains/chain/uid"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(tc.objs...).Build()
			var owner Chain = &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{}
			if tc.listener {
				owner = &configurationv1alpha1.EventGatewayListenerPolicyChain{}
			}
			holder, err := Holder(t.Context(), cl, owner, parent, tc.claim)
			require.NoError(t, err)
			require.Equal(t, tc.holds, holder != nil)
		})
	}
}
