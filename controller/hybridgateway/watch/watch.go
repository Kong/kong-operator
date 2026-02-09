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
	handler.MapFunc

	Object client.Object
}

// Watches returns a list of Watcher objects for the given resource type.
func Watches(obj client.Object, cl client.Client) []Watcher {
	switch obj.(type) {
	case *gwtypes.HTTPRoute:
		return []Watcher{
			{
				MapHTTPRouteForGateway(cl),
				&gwtypes.Gateway{},
			},
			{
				MapHTTPRouteForGatewayClass(cl),
				&gwtypes.GatewayClass{},
			},
			{
				MapHTTPRouteForService(cl),
				&corev1.Service{},
			},
			{
				MapHTTPRouteForEndpointSlice(cl),
				&discoveryv1.EndpointSlice{},
			},
			{
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongUpstream](cl),
				&configurationv1alpha1.KongUpstream{},
			},
			{
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongTarget](cl),
				&configurationv1alpha1.KongTarget{},
			},
			{
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongService](cl),
				&configurationv1alpha1.KongService{},
			},
			{
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongRoute](cl),
				&configurationv1alpha1.KongRoute{},
			},
			{
				MapHTTPRouteForKongPlugin(cl),
				&configurationv1.KongPlugin{},
			},
			{
				MapHTTPRouteForKongResource[*configurationv1alpha1.KongPluginBinding](cl),
				&configurationv1alpha1.KongPluginBinding{},
			},
			{
				MapHTTPRouteForReferenceGrant(cl),
				&gwtypes.ReferenceGrant{},
			},
		}
	case *gwtypes.Gateway:
		return []Watcher{
			{
				MapGatewayForTLSSecret(cl),
				&corev1.Secret{},
			},
			{
				MapGatewayForReferenceGrant(cl),
				&gwtypes.ReferenceGrant{},
			},
		}
	default:
		return nil
	}
}
