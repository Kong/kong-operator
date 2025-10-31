package watch

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// Watcher defines a resource and a mapping function for controller-runtime watches.
// It is used to specify which objects should be watched and how events on those objects
// should be mapped to reconciliation requests for the parent resource.
type Watcher struct {
	Object client.Object
	handler.MapFunc
}

// Watches returns a list of Watcher objects for the given resource type.
func Watches(obj client.Object, cl client.Client) []Watcher {
	switch obj.(type) {
	case *gwtypes.HTTPRoute:
		return []Watcher{
			{
				&gwtypes.Gateway{},
				MapHTTPRouteForGateway(cl),
			},
			{
				&gwtypes.GatewayClass{},
				MapHTTPRouteForGatewayClass(cl),
			},
			{
				&corev1.Service{},
				MapHTTPRouteForService(cl),
			},
			{
				&discoveryv1.EndpointSlice{},
				MapHTTPRouteForEndpointSlice(cl),
			},
		}
	default:
		return nil
	}
}
