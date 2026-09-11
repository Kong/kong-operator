package utils

import (
	"errors"
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

// errDiscoveryUnavailable is what erroringRESTMapper fails with, so that tests can
// assert the lookup error is propagated rather than just that some error occurred.
var errDiscoveryUnavailable = errors.New("discovery is unavailable")

type erroringRESTMapper struct {
	meta.RESTMapper
}

func (erroringRESTMapper) KindFor(schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, errDiscoveryUnavailable
}

func (erroringRESTMapper) KindsFor(schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, errDiscoveryUnavailable
}

func TestDetectReferenceGrantVersion(t *testing.T) {
	tests := []struct {
		name       string
		mapper     meta.RESTMapper
		expectedGV schema.GroupVersion
		// expectedErr is the sentinel the returned error must match, nil when no
		// error is expected.
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
