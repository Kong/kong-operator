package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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

	upstreamNamespace := target.Namespace
	if target.Spec.UpstreamRef.Namespace != nil && *target.Spec.UpstreamRef.Namespace != "" {
		upstreamNamespace = *target.Spec.UpstreamRef.Namespace
	}
	return []string{upstreamNamespace + "/" + target.Spec.UpstreamRef.Name}
}
