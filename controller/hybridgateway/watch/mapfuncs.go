package watch

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
)

// listHTTPRoutesForGateway returns all reconcile.Requests for HTTPRoutes referencing the given Gateway.
func listHTTPRoutesForGateway(ctx context.Context, cl client.Client, gatewayNamespace, gatewayName string) ([]reconcile.Request, error) {
	httpRoutes := &gwtypes.HTTPRouteList{}
	err := cl.List(ctx, httpRoutes, client.MatchingFields{
		index.GatewayOnHTTPRouteIndex: gatewayNamespace + "/" + gatewayName,
	})
	if err != nil {
		return nil, err
	}
	requests := make([]reconcile.Request, len(httpRoutes.Items))
	for i, httpRoute := range httpRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: httpRoute.Namespace,
				Name:      httpRoute.Name,
			},
		}
	}
	return requests, nil
}

// MapHTTPRouteForGateway returns a handler.MapFunc that, given a Gateway object,
// lists all HTTPRoutes referencing that Gateway (via ParentRefs) using the GatewayOnHTTPRouteIndex.
// It returns a slice of reconcile.Requests for each matching HTTPRoute, enabling efficient event handling
// and reconciliation when a Gateway changes.
func MapHTTPRouteForGateway(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gateway, ok := obj.(*gwtypes.Gateway)
		if !ok {
			return nil
		}
		requests, err := listHTTPRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
		if err != nil {
			return nil
		}
		return requests
	}
}

// MapHTTPRouteForGatewayClass returns a handler.MapFunc that, given a GatewayClass object,
// lists all Gateways referencing that GatewayClass (using GatewayClassOnGatewayIndex),
// then for each Gateway, lists all HTTPRoutes referencing it (via ParentRefs and GatewayOnHTTPRouteIndex).
// It returns a slice of reconcile.Requests for each matching HTTPRoute, enabling efficient event handling
// and reconciliation when a GatewayClass changes.
func MapHTTPRouteForGatewayClass(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gc, ok := obj.(*gwtypes.GatewayClass)
		if !ok {
			return nil
		}

		// List all Gateways that reference this GatewayClass using the index.
		gateways := &gwtypes.GatewayList{}
		err := cl.List(ctx, gateways, client.MatchingFields{
			index.GatewayClassOnGatewayIndex: gc.Name,
		})
		if err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, gateway := range gateways.Items {
			gwRequests, err := listHTTPRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
			if err != nil {
				return nil
			}
			requests = append(requests, gwRequests...)
		}
		return requests
	}
}

// listHTTPRoutesForService lists all HTTPRoutes that reference a specific Service using the BackendServicesOnHTTPRouteIndex.
// It returns a slice of reconcile.Requests for each matching HTTPRoute, enabling efficient event handling
// and reconciliation when a Service changes.
func listHTTPRoutesForService(ctx context.Context, cl client.Client, svcNamespace, svcName string) ([]reconcile.Request, error) {
	httpRoutes := &gwtypes.HTTPRouteList{}

	// List all HTTPRoutes that reference this Service using the index.
	err := cl.List(ctx, httpRoutes, client.MatchingFields{
		index.BackendServicesOnHTTPRouteIndex: svcNamespace + "/" + svcName,
	})
	if err != nil {
		return nil, err
	}

	requests := make([]reconcile.Request, len(httpRoutes.Items))
	for i, httpRoute := range httpRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: httpRoute.Namespace,
				Name:      httpRoute.Name,
			},
		}
	}
	return requests, nil
}

// MapHTTPRouteForService returns a handler.MapFunc that, given a Service object,
// lists all HTTPRoutes referencing that Service using the BackendServicesOnHTTPRouteIndex.
// It returns a slice of reconcile.Requests for each matching HTTPRoute, enabling efficient event handling
// and reconciliation when a Service changes.
func MapHTTPRouteForService(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return nil
		}

		requests, err := listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		if err != nil {
			return nil
		}
		return requests
	}
}

// MapHTTPRouteForEndpointSlice returns a handler.MapFunc that, given an EndpointSlice object,
// retrieves the owning Service and lists all HTTPRoutes referencing that Service using the BackendServicesOnHTTPRouteIndex.
// It returns a slice of reconcile.Requests for each matching HTTPRoute, enabling efficient event handling
// and reconciliation when an EndpointSlice changes.
func MapHTTPRouteForEndpointSlice(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		epSlice, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			return nil
		}

		// Get the Service that owns this EndpointSlice.
		svcName, ok := epSlice.Labels[discoveryv1.LabelServiceName]
		if !ok {
			return nil
		}
		svc := &corev1.Service{}
		err := cl.Get(ctx, client.ObjectKey{Namespace: epSlice.Namespace, Name: svcName}, svc)
		if err != nil {
			return nil
		}

		requests, err := listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		if err != nil {
			return nil
		}
		return requests
	}
}

// MapHTTPRouteForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// finds all HTTPRoutes in the "from" namespaces that have cross-namespace backend references
// to the ReferenceGrant's namespace. It returns a slice of reconcile.Requests for each matching
// HTTPRoute, enabling efficient event handling and reconciliation when a ReferenceGrant changes.
func MapHTTPRouteForReferenceGrant(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		rg, ok := obj.(*gwtypes.ReferenceGrant)
		if !ok {
			return nil
		}

		// For each from namespace in the ReferenceGrant, list all HTTPRoutes
		// that have cross-namespace backend refs to the ReferenceGrant's namespace.
		var requests []reconcile.Request
		for _, from := range rg.Spec.From {
			// Check that the from kind is HTTPRoute and group is gateway.networking.k8s.io.
			if from.Kind != "HTTPRoute" || (from.Group != "" && from.Group != gwtypes.GroupName) {
				continue
			}

			httpRoutes := &gwtypes.HTTPRouteList{}
			err := cl.List(ctx, httpRoutes, client.InNamespace(string(from.Namespace)))
			if err != nil {
				return nil
			}

			for _, httpRoute := range httpRoutes.Items {
				// Check if the HTTPRoute has any backend refs to the ReferenceGrant's namespace.
				hasCrossNamespaceRef := false
				for _, rule := range httpRoute.Spec.Rules {
					for _, backendRef := range rule.BackendRefs {
						// The backend must be in the ReferenceGrant's namespace (target namespace).
						if backendRef.Namespace != nil && string(*backendRef.Namespace) == rg.Namespace && httpRoute.Namespace != rg.Namespace {
							hasCrossNamespaceRef = true
							break
						}
					}
					if hasCrossNamespaceRef {
						break
					}
				}

				if hasCrossNamespaceRef {
					requests = append(requests, reconcile.Request{
						NamespacedName: client.ObjectKey{
							Namespace: httpRoute.Namespace,
							Name:      httpRoute.Name,
						},
					})
				}
			}
		}
		return requests
	}
}
