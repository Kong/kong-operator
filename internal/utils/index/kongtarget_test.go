package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// TestKongTargetReferencesKongUpstream covers the index key builder for the
// KongTarget -> KongUpstream relation. The key must be "<upstreamNamespace>/<upstreamName>"
// so that lookups match cross-namespace upstreams as well as same-namespace ones.
func TestKongTargetReferencesKongUpstream(t *testing.T) {
	const (
		targetNamespace = "target-ns"
		upstreamName    = "upstream"
		otherNS         = "upstream-ns"
	)

	testCases := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "non-KongTarget object returns nil",
			obj:  &configurationv1alpha1.KongUpstream{},
			want: nil,
		},
		{
			name: "same-namespace upstreamRef (no namespace field) keys with target's namespace",
			obj: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{Namespace: targetNamespace},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NamespacedRef{
						Name: upstreamName,
					},
				},
			},
			want: []string{targetNamespace + "/" + upstreamName},
		},
		{
			name: "cross-namespace upstreamRef keys with upstreamRef's namespace",
			obj: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{Namespace: targetNamespace},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NamespacedRef{
						Name:      upstreamName,
						Namespace: new(otherNS),
					},
				},
			},
			want: []string{otherNS + "/" + upstreamName},
		},
		{
			name: "empty namespace pointer falls back to target's namespace",
			obj: &configurationv1alpha1.KongTarget{
				ObjectMeta: metav1.ObjectMeta{Namespace: targetNamespace},
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: commonv1alpha1.NamespacedRef{
						Name:      upstreamName,
						Namespace: new(""),
					},
				},
			},
			want: []string{targetNamespace + "/" + upstreamName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, kongTargetReferencesKongUpstream(tc.obj))
		})
	}
}
