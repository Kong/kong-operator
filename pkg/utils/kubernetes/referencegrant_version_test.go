package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func restMapperWithReferenceGrant(gvs ...schema.GroupVersion) meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper(gvs)
	for _, gv := range gvs {
		mapper.Add(gv.WithKind("ReferenceGrant"), meta.RESTScopeNamespace)
	}
	return mapper
}

func TestDetectReferenceGrantVersion(t *testing.T) {
	tests := []struct {
		name        string
		mapper      meta.RESTMapper
		expectedGV  schema.GroupVersion
		expectedErr error
	}{
		{
			name:        "lookup failure is reported as an error",
			mapper:      erroringRESTMapper{RESTMapper: meta.NewDefaultRESTMapper(nil)},
			expectedGV:  schema.GroupVersion{},
			expectedErr: errDiscoveryUnavailable,
		},
		{
			name:       "only v1 is served",
			mapper:     restMapperWithReferenceGrant(v1GroupVersion),
			expectedGV: v1GroupVersion,
		},
		{
			name:       "only v1beta1 is served",
			mapper:     restMapperWithReferenceGrant(v1beta1GroupVersion),
			expectedGV: v1beta1GroupVersion,
		},
		{
			name:       "both versions served, v1 is preferred",
			mapper:     restMapperWithReferenceGrant(v1GroupVersion, v1beta1GroupVersion),
			expectedGV: v1GroupVersion,
		},
		{
			name:        "neither version served",
			mapper:      restMapperWithReferenceGrant(),
			expectedGV:  schema.GroupVersion{},
			expectedErr: ErrReferenceGrantCRDNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gv, err := DetectReferenceGrantVersion(tc.mapper)
			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.expectedGV, gv)
		})
	}
}

func TestNewReferenceGrant(t *testing.T) {
	require.IsType(t, &gatewayv1beta1.ReferenceGrant{}, NewReferenceGrant(v1beta1GroupVersion))
	require.IsType(t, &gwtypes.ReferenceGrant{}, NewReferenceGrant(v1GroupVersion))
	require.IsType(t, &gwtypes.ReferenceGrant{}, NewReferenceGrant(schema.GroupVersion{}), "zero value must default to v1")
}

func TestNewReferenceGrantList(t *testing.T) {
	require.IsType(t, &gatewayv1beta1.ReferenceGrantList{}, NewReferenceGrantList(v1beta1GroupVersion))
	require.IsType(t, &gwtypes.ReferenceGrantList{}, NewReferenceGrantList(v1GroupVersion))
	require.IsType(t, &gwtypes.ReferenceGrantList{}, NewReferenceGrantList(schema.GroupVersion{}), "zero value must default to v1")
}

func TestAsReferenceGrant(t *testing.T) {
	v1Grant := &gwtypes.ReferenceGrant{Spec: gwtypes.ReferenceGrantSpec{From: []gwtypes.ReferenceGrantFrom{{Kind: "HTTPRoute"}}}}
	got, ok := AsReferenceGrant(v1Grant)
	require.True(t, ok)
	require.Same(t, v1Grant, got)

	v1beta1Grant := &gatewayv1beta1.ReferenceGrant{Spec: gwtypes.ReferenceGrantSpec{From: []gwtypes.ReferenceGrantFrom{{Kind: "TCPRoute"}}}}
	got, ok = AsReferenceGrant(v1beta1Grant)
	require.True(t, ok)
	require.Equal(t, "TCPRoute", string(got.Spec.From[0].Kind))

	_, ok = AsReferenceGrant(&gwtypes.Gateway{})
	require.False(t, ok)
}

func TestReferenceGrantItems(t *testing.T) {
	v1List := &gwtypes.ReferenceGrantList{Items: []gwtypes.ReferenceGrant{
		{Spec: gwtypes.ReferenceGrantSpec{From: []gwtypes.ReferenceGrantFrom{{Kind: "HTTPRoute"}}}},
	}}
	items := ReferenceGrantItems(v1List)
	require.Len(t, items, 1)
	require.Equal(t, "HTTPRoute", string(items[0].Spec.From[0].Kind))

	v1beta1List := &gatewayv1beta1.ReferenceGrantList{Items: []gatewayv1beta1.ReferenceGrant{
		{Spec: gwtypes.ReferenceGrantSpec{From: []gwtypes.ReferenceGrantFrom{{Kind: "TCPRoute"}}}},
	}}
	items = ReferenceGrantItems(v1beta1List)
	require.Len(t, items, 1)
	require.Equal(t, "TCPRoute", string(items[0].Spec.From[0].Kind))

	require.Nil(t, ReferenceGrantItems(&gwtypes.GatewayList{}))
}
