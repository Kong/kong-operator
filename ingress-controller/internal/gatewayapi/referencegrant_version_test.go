package gatewayapi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

var (
	testV1GroupVersion      = schema.GroupVersion(gatewayv1.GroupVersion)
	testV1beta1GroupVersion = schema.GroupVersion(gatewayv1beta1.GroupVersion)
)

func TestNewReferenceGrant(t *testing.T) {
	require.IsType(t, &gatewayv1beta1.ReferenceGrant{}, NewReferenceGrant(testV1beta1GroupVersion))
	require.IsType(t, &ReferenceGrant{}, NewReferenceGrant(testV1GroupVersion))
	require.IsType(t, &ReferenceGrant{}, NewReferenceGrant(schema.GroupVersion{}), "zero value must default to v1")
}

func TestNewReferenceGrantList(t *testing.T) {
	require.IsType(t, &gatewayv1beta1.ReferenceGrantList{}, NewReferenceGrantList(testV1beta1GroupVersion))
	require.IsType(t, &ReferenceGrantList{}, NewReferenceGrantList(testV1GroupVersion))
	require.IsType(t, &ReferenceGrantList{}, NewReferenceGrantList(schema.GroupVersion{}), "zero value must default to v1")
}

func TestAsReferenceGrant(t *testing.T) {
	v1Grant := &ReferenceGrant{Spec: ReferenceGrantSpec{From: []ReferenceGrantFrom{{Kind: "HTTPRoute"}}}}
	got, ok := AsReferenceGrant(v1Grant)
	require.True(t, ok)
	require.Same(t, v1Grant, got)

	v1beta1Grant := &gatewayv1beta1.ReferenceGrant{Spec: ReferenceGrantSpec{From: []ReferenceGrantFrom{{Kind: "TCPRoute"}}}}
	got, ok = AsReferenceGrant(v1beta1Grant)
	require.True(t, ok)
	require.Equal(t, "TCPRoute", string(got.Spec.From[0].Kind))

	_, ok = AsReferenceGrant(&Gateway{})
	require.False(t, ok)
}

func TestReferenceGrantItems(t *testing.T) {
	v1List := &ReferenceGrantList{Items: []ReferenceGrant{
		{Spec: ReferenceGrantSpec{From: []ReferenceGrantFrom{{Kind: "HTTPRoute"}}}},
	}}
	items := ReferenceGrantItems(v1List)
	require.Len(t, items, 1)
	require.Equal(t, "HTTPRoute", string(items[0].Spec.From[0].Kind))

	v1beta1List := &gatewayv1beta1.ReferenceGrantList{Items: []gatewayv1beta1.ReferenceGrant{
		{Spec: ReferenceGrantSpec{From: []ReferenceGrantFrom{{Kind: "TCPRoute"}}}},
	}}
	items = ReferenceGrantItems(v1beta1List)
	require.Len(t, items, 1)
	require.Equal(t, "TCPRoute", string(items[0].Spec.From[0].Kind))

	require.Nil(t, ReferenceGrantItems(&GatewayList{}))
}
