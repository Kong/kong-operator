package watch

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
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
func Watches(obj client.Object, cl client.Client, referenceGrantEnabled bool) []Watcher {
	switch obj.(type) {
	case *gwtypes.HTTPRoute:
		watcher := []Watcher{
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
			{
				&configurationv1alpha1.KongUpstream{},
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongUpstream](cl),
			},
			{
				&configurationv1alpha1.KongTarget{},
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongTarget](cl),
			},
			{
				&configurationv1alpha1.KongService{},
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongService](cl),
			},
			{
				&configurationv1alpha1.KongRoute{},
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongRoute](cl),
			},
			{
				&configurationv1.KongPlugin{},
				MapHTTPRouteForKongResource[*configurationv1.KongPlugin](cl),
			},
			{
				&configurationv1alpha1.KongPluginBinding{},
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongPluginBinding](cl),
			},
		}

		if referenceGrantEnabled {
			watcher = append(watcher, Watcher{
				&gwtypes.ReferenceGrant{},
				MapHTTPRouteForReferenceGrant(cl),
			})
		}
		return watcher
	default:
		return nil
	}
}
