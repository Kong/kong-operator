package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
)

func TestCRDChecker(t *testing.T) {
	testcases := []struct {
		name        string
		restMapper  func() meta.RESTMapper
		CRD         schema.GroupVersionResource
		expected    bool
		expectedErr error
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
					Group:   gatewayv1.SchemeGroupVersion.Group,
					Version: gatewayv1.SchemeGroupVersion.Version,
					Kind:    "Gateway",
				}, meta.RESTScopeNamespace)

				return meta.NewDefaultRESTMapper(nil)
			},
			CRD: schema.GroupVersionResource{
				Group:   gatewayv1.SchemeGroupVersion.Group,
				Version: gatewayv1.SchemeGroupVersion.Version,
				// Note: pluralising gateways does not work hence we need this.
				// Ref: https://github.com/kubernetes/client-go/issues/1082
				Resource: "gatewaies",
			},
			expected:    false,
			expectedErr: nil,
		},
		{
			name: "returns false when Gateway CRD is not installed",
			restMapper: func() meta.RESTMapper {
				return meta.NewDefaultRESTMapper(nil)
			},
			CRD: schema.GroupVersionResource{
				Group:   gatewayv1.SchemeGroupVersion.Group,
				Version: gatewayv1.SchemeGroupVersion.Version,
				// Note: pluralising gateways does not work hence we need this.
				// Ref: https://github.com/kubernetes/client-go/issues/1082
				Resource: "gatewaies",
			},
			expected:    false,
			expectedErr: nil,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithRESTMapper(tc.restMapper()).
				Build()

			checker := CRDChecker{Client: fakeClient}
			ok, err := checker.CRDExists(tc.CRD)

			if tc.expectedErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expectedErr)
			} else {
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

		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithRESTMapper(restMapper).
			Build()

		checker := CRDChecker{Client: fakeClient}
		gvr := operatorv1beta1.DataPlaneGVR()

		b.ResetTimer()
		for b.Loop() {
			_, _ = checker.CRDExists(gvr)
		}
	})

	b.Run("CRD_not_found", func(b *testing.B) {
		restMapper := meta.NewDefaultRESTMapper(nil)

		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithRESTMapper(restMapper).
			Build()

		checker := CRDChecker{Client: fakeClient}
		gvr := operatorv1beta1.DataPlaneGVR()

		b.ResetTimer()
		for b.Loop() {
			_, _ = checker.CRDExists(gvr)
		}
	})
}
