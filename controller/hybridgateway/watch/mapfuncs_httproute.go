package watch

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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

// MapHTTPRouteForKongResource returns a handler.MapFunc that, given a Kong resource object of type T,
// retrieves the HTTPRoutes referenced in its annotations. It returns a slice of reconcile.Requests
// for each matching HTTPRoute.
func MapHTTPRouteForKongResource[T kongResource](cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		_, ok := obj.(T)
		if !ok {
			return nil
		}

		am := metadata.NewAnnotationManager(logr.Discard())
		routes := am.GetRoutes(obj)
		if len(routes) == 0 {
			return nil
		}

		var requests []reconcile.Request
		for _, r := range routes {
			requests = append(requests, reconcile.Request{
				NamespacedName: metadata.NameStringToObjectKey(r),
			})
		}
		return requests
	}
}

// MapHTTPRouteForKongPlugin returns a handler.MapFunc that, given a KongPlugin object,
// lists all HTTPRoutes that reference it. This includes both:
// 1. HTTPRoutes that explicitly reference the KongPlugin via the konghq.com/plugins annotation
// 2. HTTPRoutes that have generated KongPlugins from Gateway API extensionRef filters
// It returns a slice of reconcile.Requests for each matching HTTPRoute, enabling efficient
// event handling and reconciliation when a KongPlugin changes.
func MapHTTPRouteForKongPlugin(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		_, ok := obj.(*configurationv1.KongPlugin)
		if !ok {
			return nil
		}

		// List all HTTPRoutes that reference this plugin using the index.
		httpRoutes := &gwtypes.HTTPRouteList{}
		plugin := obj.(*configurationv1.KongPlugin)
		err := cl.List(ctx, httpRoutes, client.MatchingFields{
			index.KongPluginsOnHTTPRouteIndex: plugin.Namespace + "/" + plugin.Name,
		})
		if err != nil {
			return nil
		}

		// Add requests for HTTPRoutes found via the index.
		indexRequests := make([]reconcile.Request, len(httpRoutes.Items))
		for i, httpRoute := range httpRoutes.Items {
			indexRequests[i] = reconcile.Request{
				NamespacedName: client.ObjectKey{
					Namespace: httpRoute.Namespace,
					Name:      httpRoute.Name,
				},
			}
		}

		// Add requests for Plugins referencing the HTTPRoute via annotation.
		requests := MapRouteForKongResource[*configurationv1.KongPlugin](cl)(ctx, obj)
		return append(requests, indexRequests...)
	}
}
