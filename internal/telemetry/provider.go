package telemetry

import (
	"github.com/kong/kubernetes-telemetry/pkg/provider"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
)

const (
	// DataPlaneK8sResourceName is the registered name of resource in kubernetes for dataplanes.
	DataPlaneK8sResourceName = "dataplanes"
	// DataPlaneCountKind is the kind of provider reporting number of dataplanes.
	DataPlaneCountKind = provider.Kind("dataplanes_count")
)

var (
	dataplaneGVR = schema.GroupVersionResource{
		Group:    operatorv1beta1.SchemeGroupVersion.Group,
		Version:  operatorv1beta1.SchemeGroupVersion.Version,
		Resource: DataPlaneK8sResourceName,
	}
)

// NewDataPlaneCountProvider creates a provider for number of dataplanes in the cluster.
func NewDataPlaneCountProvider(dyn dynamic.Interface, restMapper meta.RESTMapper) (provider.Provider, error) {
	return provider.NewK8sObjectCountProviderWithRESTMapper(
		DataPlaneK8sResourceName, DataPlaneCountKind, dyn, dataplaneGVR, restMapper,
	)
}
