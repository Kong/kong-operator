package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
)

const (
	// IndexFieldKongTargetOnReferencedUpstream is the index field for KongTarget -> KongUpstream.
	IndexFieldKongTargetOnReferencedUpstream = "kongTargetUpstreamRef"
)

// OptionsForKongTarget returns required Index options for KongTarget reconciler.
func OptionsForKongTarget() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongTarget{},
			Field:          IndexFieldKongTargetOnReferencedUpstream,
			ExtractValueFn: kongTargetReferencesKongUpstream,
		},
	}
}

func kongTargetReferencesKongUpstream(object client.Object) []string {
	target, ok := object.(*configurationv1alpha1.KongTarget)
	if !ok {
		return nil
	}

	return []string{target.Spec.UpstreamRef.Name}
}
