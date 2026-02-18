package telemetry

import (
	"github.com/kong/kubernetes-telemetry/pkg/provider"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
)

// NewObjectCountProvider creates a provider for number of objects in the cluster.
func NewObjectCountProvider[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](dyn dynamic.Interface, restMapper meta.RESTMapper, group string, version string) (provider.Provider, error) {
	kind := constraints.EntityTypeName[T]()
	restMapping, err := restMapper.RESTMapping(
		schema.GroupKind{
			Group: group,
			Kind:  kind,
		},
		version,
	)
	if err != nil {
		return nil, err
	}

	gvr := schema.GroupVersionResource{
		Group:    restMapping.Resource.Group,
		Version:  restMapping.Resource.Version,
		Resource: restMapping.Resource.Resource,
	}

	return provider.NewK8sObjectCountProviderWithRESTMapper(
		restMapping.Resource.Resource,
		provider.Kind(kind),
		dyn,
		gvr,
		restMapper,
	)
}
