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

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// errListClient returns an error on every List call.
type errListClient struct{ client.Client }

func (c *errListClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return assert.AnError
}

func Test_enqueueForKonnectEventGatewayRef(t *testing.T) {
	const (
		ns      = "test-ns"
		kegName = "my-keg"
	)

	keg := &konnectv1alpha1.KonnectEventControlPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: kegName},
	}

	egdpMatching := &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "dp-match"},
		Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
			ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
				KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{Name: kegName},
			},
		},
	}

	egdpOther := &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "dp-other"},
		Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
			ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
				KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{Name: "other-keg"},
			},
		},
	}

	scheme := managerscheme.Get()

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(keg, egdpMatching, egdpOther).
		WithIndex(
			&eventgatewayv1alpha1.KegDataPlane{},
			index.IndexFieldKegDataPlaneOnKonnectEventGateway,
			func(obj client.Object) []string {
				dp, ok := obj.(*eventgatewayv1alpha1.KegDataPlane)
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
			obj:           keg,
			wantNames:     []string{"dp-match"},
			wantNamespace: ns,
		},
		{
			name:    "returns nil when obj is not KonnectEventGateway",
			cl:      cl,
			obj:     &corev1.ConfigMap{},
			wantNil: true,
		},
		{
			name:    "returns nil when List fails",
			cl:      &errListClient{},
			obj:     keg,
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapFunc := enqueueForKonnectEventGatewayRef(tc.cl)
			requests := mapFunc(context.Background(), tc.obj)
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
