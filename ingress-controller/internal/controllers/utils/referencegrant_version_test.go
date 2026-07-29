package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

var (
	v1GroupVersion      = schema.GroupVersion(gatewayv1.GroupVersion)
	v1beta1GroupVersion = schema.GroupVersion(gatewayv1beta1.GroupVersion)
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
		name       string
		mapper     meta.RESTMapper
		expectedGV schema.GroupVersion
		expectedOK bool
	}{
		{
			name:       "only v1 is served",
			mapper:     restMapperWithReferenceGrant(v1GroupVersion),
			expectedGV: v1GroupVersion,
			expectedOK: true,
		},
		{
			name:       "only v1beta1 is served",
			mapper:     restMapperWithReferenceGrant(v1beta1GroupVersion),
			expectedGV: v1beta1GroupVersion,
			expectedOK: true,
		},
		{
			name:       "both versions served, v1 is preferred",
			mapper:     restMapperWithReferenceGrant(v1GroupVersion, v1beta1GroupVersion),
			expectedGV: v1GroupVersion,
			expectedOK: true,
		},
		{
			name:       "neither version served",
			mapper:     restMapperWithReferenceGrant(),
			expectedGV: schema.GroupVersion{},
			expectedOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gv, ok := DetectReferenceGrantVersion(tc.mapper)
			require.Equal(t, tc.expectedOK, ok)
			require.Equal(t, tc.expectedGV, gv)
		})
	}
}
