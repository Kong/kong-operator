package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongTargetOnReferencedUpstream is the index field for KongTarget -> KongUpstream.
	IndexFieldKongTargetOnReferencedUpstream = "kongTargetUpstreamRef"
)

// IndexOptionsForKongTarget returns required Index options for KongTarget reconciler.
func IndexOptionsForKongTarget() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongTarget{},
			IndexField:   IndexFieldKongTargetOnReferencedUpstream,
			ExtractValue: kongTargetReferencesKongUpstream,
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
