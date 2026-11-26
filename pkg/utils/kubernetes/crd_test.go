package kubernetes

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

var errDiscoveryUnavailable = errors.New("discovery is unavailable")

type erroringRESTMapper struct {
	meta.RESTMapper
}

func (erroringRESTMapper) KindFor(schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, errDiscoveryUnavailable
}

// errGroupDiscoveryFailedRESTMapper always fails the lookup with a
// discovery.ErrGroupDiscoveryFailed for the given groups.
type errGroupDiscoveryFailedRESTMapper struct {
	meta.RESTMapper

	failedGroups map[schema.GroupVersion]error
}

func (m errGroupDiscoveryFailedRESTMapper) KindFor(
	schema.GroupVersionResource,
) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, &discovery.ErrGroupDiscoveryFailed{Groups: m.failedGroups}
}

func notFoundStatus(gvr schema.GroupVersionResource) *apierrors.StatusError {
	return apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, "thing")
}

func TestCRDExists(t *testing.T) {
	testcases := []struct {
		name                string
		restMapper          func() meta.RESTMapper
		CRD                 schema.GroupVersionResource
		expected            bool
		expectedErr         error
		expectedErrContains string
	}{
		{
			name: "DataPlane CRD is found when installed",
			restMapper: func() meta.RESTMapper {
				restMapper := meta.NewDefaultRESTMapper(nil)

				restMapper.Add(schema.GroupVersionKind{
					Group:   operatorv1beta1.SchemeGroupVersion.Group,
					Version: operatorv1beta1.SchemeGroupVersion.Version,
					Kind:    "DataPlane",
				}, meta.RESTScopeNamespace)

				return restMapper
			},
			CRD:         operatorv1beta1.DataPlaneGVR(),
			expected:    true,
			expectedErr: nil,
		},
		{
			name: "returns false when DataPlane CRD is not installed",
			restMapper: func() meta.RESTMapper {
				return meta.NewDefaultRESTMapper(nil)
			},
			CRD:         operatorv1beta1.DataPlaneGVR(),
			expected:    false,
			expectedErr: nil,
		},
		{
			name: "Gateway CRD is found when installed",
			restMapper: func() meta.RESTMapper {
				restMapper := meta.NewDefaultRESTMapper(nil)

				restMapper.Add(schema.GroupVersionKind{
					Group:   gatewayv1.GroupVersion.Group,
					Version: gatewayv1.GroupVersion.Version,
					Kind:    "Gateway",
				}, meta.RESTScopeNamespace)

				return restMapper
			},
			CRD: schema.GroupVersionResource{
				Group:   gatewayv1.GroupVersion.Group,
				Version: gatewayv1.GroupVersion.Version,
				// Note: pluralising gateways does not work hence we need this.
				// Ref: https://github.com/kubernetes/client-go/issues/1082
				Resource: "gatewaies",
			},
			expected:    true,
			expectedErr: nil,
		},
		{
			name: "returns false when Gateway CRD is not installed",
			restMapper: func() meta.RESTMapper {
				return meta.NewDefaultRESTMapper(nil)
			},
			CRD: schema.GroupVersionResource{
				Group:   gatewayv1.GroupVersion.Group,
				Version: gatewayv1.GroupVersion.Version,
				// Note: pluralising gateways does not work hence we need this.
				// Ref: https://github.com/kubernetes/client-go/issues/1082
				Resource: "gatewaies",
			},
			expected:    false,
			expectedErr: nil,
		},
		{
			name: "returns false without error when the requested group gets a 404 during discovery, and an unrelated group fails",
			restMapper: func() meta.RESTMapper {
				gvr := operatorv1beta1.DataPlaneGVR()
				broken := schema.GroupVersionResource{Group: "broken.example.com", Version: "v1", Resource: "things"}
				return errGroupDiscoveryFailedRESTMapper{
					RESTMapper: meta.NewDefaultRESTMapper(nil),
					failedGroups: map[schema.GroupVersion]error{
						gvr.GroupVersion():    notFoundStatus(gvr),
						broken.GroupVersion(): errors.New("some unrelated failure"),
					},
				}
			},
			CRD:         operatorv1beta1.DataPlaneGVR(),
			expected:    false,
			expectedErr: nil,
		},
		{
			name: "returns an error when the requested group is not among the discovery failures",
			restMapper: func() meta.RESTMapper {
				broken := schema.GroupVersionResource{Group: "broken.example.com", Version: "v1", Resource: "things"}
				return errGroupDiscoveryFailedRESTMapper{
					RESTMapper:   meta.NewDefaultRESTMapper(nil),
					failedGroups: map[schema.GroupVersion]error{broken.GroupVersion(): notFoundStatus(broken)},
				}
			},
			CRD:                 operatorv1beta1.DataPlaneGVR(),
			expected:            false,
			expectedErrContains: "unable to retrieve the complete list of server APIs",
		},
		{
			name: "returns an error when the lookup itself fails",
			restMapper: func() meta.RESTMapper {
				return erroringRESTMapper{RESTMapper: meta.NewDefaultRESTMapper(nil)}
			},
			CRD:         operatorv1beta1.DataPlaneGVR(),
			expected:    false,
			expectedErr: errDiscoveryUnavailable,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := CRDExists(tc.restMapper(), tc.CRD)

			switch {
			case tc.expectedErrContains != "":
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectedErrContains)
			case tc.expectedErr != nil:
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expectedErr)
			default:
				require.NoError(t, err)
			}

			if tc.expected {
				require.True(t, ok)
			} else {
				require.False(t, ok)
			}
		})
	}
}

func BenchmarkCRDExists(b *testing.B) {
	b.Run("CRD_found", func(b *testing.B) {
		restMapper := meta.NewDefaultRESTMapper(nil)
		restMapper.Add(schema.GroupVersionKind{
			Group:   operatorv1beta1.SchemeGroupVersion.Group,
			Version: operatorv1beta1.SchemeGroupVersion.Version,
			Kind:    "DataPlane",
		}, meta.RESTScopeNamespace)

		gvr := operatorv1beta1.DataPlaneGVR()

		b.ResetTimer()
		for b.Loop() {
			_, _ = CRDExists(restMapper, gvr)
		}
	})

	b.Run("CRD_not_found", func(b *testing.B) {
		restMapper := meta.NewDefaultRESTMapper(nil)

		gvr := operatorv1beta1.DataPlaneGVR()

		b.ResetTimer()
		for b.Loop() {
			_, _ = CRDExists(restMapper, gvr)
		}
	})
}
