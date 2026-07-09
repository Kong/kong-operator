package dataplane

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// errListClient returns an error on every List call.
type errListClient struct{ client.Client }

func (c *errListClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return assert.AnError
}

func Test_enqueueForAIGatewayControlPlaneRef(t *testing.T) {
	const (
		ns       = "test-ns"
		aigwcpNM = "my-aigwcp"
	)

	aigwcp := &konnectv1alpha1.AIGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: aigwcpNM},
	}

	aigwdpMatching := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "dp-match"},
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
				KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{Name: aigwcpNM},
			},
		},
	}

	aigwdpOther := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "dp-other"},
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
				KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{Name: "other-aigwcp"},
			},
		},
	}

	scheme := managerscheme.Get()

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(aigwcp, aigwdpMatching, aigwdpOther).
		WithIndex(
			&aigatewayv1alpha1.AIGatewayDataPlane{},
			index.IndexFieldAIGatewayDataPlaneOnAIGatewayControlPlane,
			func(obj client.Object) []string {
				dp, ok := obj.(*aigatewayv1alpha1.AIGatewayDataPlane)
				if !ok || dp.Spec.ControlPlaneRef.KonnectNamespacedRef == nil {
					return nil
				}
				return []string{dp.Namespace + "/" + dp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name}
			},
		).
		Build()

	tests := []struct {
		name          string
		cl            client.Client
		obj           client.Object
		wantNil       bool
		wantNames     []string
		wantNamespace string
	}{
		{
			name:          "returns requests for matching DataPlanes",
			cl:            cl,
			obj:           aigwcp,
			wantNames:     []string{"dp-match"},
			wantNamespace: ns,
		},
		{
			name:    "returns nil when obj is not AIGatewayControlPlane",
			cl:      cl,
			obj:     &corev1.ConfigMap{},
			wantNil: true,
		},
		{
			name:    "returns nil when List fails",
			cl:      &errListClient{},
			obj:     aigwcp,
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapFunc := enqueueForAIGatewayControlPlaneRef(tc.cl)
			requests := mapFunc(t.Context(), tc.obj)
			if tc.wantNil {
				require.Nil(t, requests)
				return
			}
			require.Len(t, requests, len(tc.wantNames))
			for i, name := range tc.wantNames {
				assert.Equal(t, tc.wantNamespace, requests[i].Namespace)
				assert.Equal(t, name, requests[i].Name)
			}
		})
	}
}
