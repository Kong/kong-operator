package watch

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
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
				MapRouteForGateway(cl, gwtypes.HTTPRoute{}),
				&gwtypes.Gateway{},
			},
			{
				MapRouteForGatewayClass(cl, gwtypes.HTTPRoute{}),
				&gwtypes.GatewayClass{},
			},
			{
				MapRouteForService(cl, gwtypes.HTTPRoute{}),
				&corev1.Service{},
			},
			{
				MapRouteForEndpointSlice(cl, gwtypes.HTTPRoute{}),
				&discoveryv1.EndpointSlice{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongUpstream](kindHTTPRoute),
				&configurationv1alpha1.KongUpstream{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongTarget](kindHTTPRoute),
				&configurationv1alpha1.KongTarget{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongService](kindHTTPRoute),
				&configurationv1alpha1.KongService{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongRoute](kindHTTPRoute),
				&configurationv1alpha1.KongRoute{},
			},
			{
				MapHTTPRouteForKongPlugin(cl),
				&configurationv1.KongPlugin{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongPluginBinding](kindHTTPRoute),
				&configurationv1alpha1.KongPluginBinding{},
			},
			{
				MapHTTPRouteForReferenceGrant(cl),
				&gwtypes.ReferenceGrant{},
			},
			{
				MapHTTPRouteForKongUpstreamPolicy(cl),
				&configurationv1beta1.KongUpstreamPolicy{},
			},
			{
				MapHTTPRouteForClientCertSecret(cl),
				&corev1.Secret{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongCertificate](kindHTTPRoute),
				&configurationv1alpha1.KongCertificate{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongReferenceGrant](kindHTTPRoute),
				&configurationv1alpha1.KongReferenceGrant{},
			},
		}
	case *gwtypes.TLSRoute:
		return []Watcher{
			{
				MapRouteForGateway(cl, gwtypes.TLSRoute{}),
				&gwtypes.Gateway{},
			},
			{
				MapRouteForGatewayClass(cl, gwtypes.TLSRoute{}),
				&gwtypes.GatewayClass{},
			},
			{
				MapRouteForService(cl, gwtypes.TLSRoute{}),
				&corev1.Service{},
			},
			{
				MapRouteForEndpointSlice(cl, gwtypes.TLSRoute{}),
				&discoveryv1.EndpointSlice{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongUpstream](kindTLSRoute),
				&configurationv1alpha1.KongUpstream{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongTarget](kindTLSRoute),
				&configurationv1alpha1.KongTarget{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongService](kindTLSRoute),
				&configurationv1alpha1.KongService{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongRoute](kindTLSRoute),
				&configurationv1alpha1.KongRoute{},
			},
			{
				MapTLSRouteForReferenceGrant(cl),
				&gwtypes.ReferenceGrant{},
			},
			{
				MapTLSRouteForClientCertSecret(cl),
				&corev1.Secret{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongCertificate](kindTLSRoute),
				&configurationv1alpha1.KongCertificate{},
			},
			{
				MapRouteForKongResource[*configurationv1alpha1.KongReferenceGrant](kindTLSRoute),
				&configurationv1alpha1.KongReferenceGrant{},
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
