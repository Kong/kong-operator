package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// TestKongRouteRefersToKongService covers the index key builder for the
// KongRoute -> KongService relation. The key must be "<svcNamespace>/<svcName>"
// so that lookups match cross-namespace services as well as same-namespace ones.
func TestKongRouteRefersToKongService(t *testing.T) {
	const (
		routeNamespace = "route-ns"
		serviceName    = "svc"
	)
	otherNamespace := "svc-ns"

	testCases := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "non-route object returns nil",
			obj:  &configurationv1alpha1.KongService{},
			want: nil,
		},
		{
			name: "route without serviceRef returns nil",
			obj: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: routeNamespace},
			},
			want: nil,
		},
		{
			name: "route with non-namespacedRef type returns nil",
			obj: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: routeNamespace},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: "konnectID",
					},
				},
			},
			want: nil,
		},
		{
			name: "route with nil NamespacedRef returns nil",
			obj: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: routeNamespace},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
					},
				},
			},
			want: nil,
		},
		{
			name: "same-namespace serviceRef (no namespace field) keys with route's namespace",
			obj: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: routeNamespace},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: serviceName,
						},
					},
				},
			},
			want: []string{routeNamespace + "/" + serviceName},
		},
		{
			name: "cross-namespace serviceRef keys with serviceRef's namespace",
			obj: &configurationv1alpha1.KongRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: routeNamespace},
				Spec: configurationv1alpha1.KongRouteSpec{
					ServiceRef: &configurationv1alpha1.ServiceRef{
						Type: configurationv1alpha1.ServiceRefNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name:      serviceName,
							Namespace: &otherNamespace,
						},
					},
				},
			},
			want: []string{otherNamespace + "/" + serviceName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, kongRouteRefersToKongService(tc.obj))
		})
	}
}
