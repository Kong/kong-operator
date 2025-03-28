package konnect

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/kong/gateway-operator/internal/utils/index"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KonnectCloudGatewayDataPlaneGroupConfigurationReconciliationWatchOptions returns
// the watch options for the KonnectCloudGatewayDataPlaneGroupConfiguration.
func KonnectCloudGatewayDataPlaneGroupConfigurationReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{})
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationList](
						cl, index.IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane,
					),
				),
			)
		},
	}
}
