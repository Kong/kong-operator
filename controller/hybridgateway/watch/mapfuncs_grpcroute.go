package watch

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// listGRPCRoutesForGateway returns all reconcile.Requests for GRPCRoutes referencing the given Gateway.
func listGRPCRoutesForGateway(ctx context.Context, cl client.Client, gatewayNamespace, gatewayName string) ([]reconcile.Request, error) {
	grpcRoutes := &gwtypes.GRPCRouteList{}
	err := cl.List(ctx, grpcRoutes, client.MatchingFields{
		index.GatewayOnGRPCRouteIndex: gatewayNamespace + "/" + gatewayName,
	})
	if err != nil {
		return nil, err
	}
	requests := make([]reconcile.Request, len(grpcRoutes.Items))
	for i, grpcRoute := range grpcRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: grpcRoute.Namespace,
				Name:      grpcRoute.Name,
			},
		}
	}
	return requests, nil
}

// listGRPCRoutesForService lists all GRPCRoutes that reference a specific Service using the BackendServicesOnGRPCRouteIndex.
// It returns a slice of reconcile.Requests for each matching GRPCRoute, enabling efficient event handling
// and reconciliation when a Service changes.
func listGRPCRoutesForService(ctx context.Context, cl client.Client, svcNamespace, svcName string) ([]reconcile.Request, error) {
	grpcRoutes := &gwtypes.GRPCRouteList{}

	// List all GRPCRoutes that reference this Service using the index.
	err := cl.List(ctx, grpcRoutes, client.MatchingFields{
		index.BackendServicesOnGRPCRouteIndex: svcNamespace + "/" + svcName,
	})
	if err != nil {
		return nil, err
	}

	requests := make([]reconcile.Request, len(grpcRoutes.Items))
	for i, grpcRoute := range grpcRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: grpcRoute.Namespace,
				Name:      grpcRoute.Name,
			},
		}
	}

	return requests, nil
}

// MapGRPCRouteForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// finds all GRPCRoutes in the "from" namespaces that have cross-namespace backend references
// to the ReferenceGrant's namespace. It returns a slice of reconcile.Requests for each matching
// GRPCRoute, enabling efficient event handling and reconciliation when a ReferenceGrant changes.
func MapGRPCRouteForReferenceGrant(cl client.Client) handler.MapFunc {
	return MapRouteForReferenceGrant[gwtypes.GRPCRouteList](cl)
}

// MapGRPCRouteForKongPlugin returns a handler.MapFunc that, given a KongPlugin object,
// lists all GRPCRoutes that reference it. This includes both:
// 1. GRPCRoutes that explicitly reference the KongPlugin via the konghq.com/plugins annotation
// 2. GRPCRoutes that have generated KongPlugins from Gateway API extensionRef filters
// It returns a slice of reconcile.Requests for each matching GRPCRoute, enabling efficient
// event handling and reconciliation when a KongPlugin changes.
func MapGRPCRouteForKongPlugin(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		_, ok := obj.(*configurationv1.KongPlugin)
		if !ok {
			return nil
		}

		// List all GRPCRoutes that reference this plugin using the index.
		grpcRoutes := &gwtypes.GRPCRouteList{}
		plugin := obj.(*configurationv1.KongPlugin)
		err := cl.List(ctx, grpcRoutes, &client.MatchingFields{
			index.KongPluginsOnGRPCRouteIndex: plugin.Namespace + "/" + plugin.Name,
		})
		if err != nil {
			return nil
		}

		// Add requests for GRPCRoutes found via the index.
		indexRequests := make([]reconcile.Request, len(grpcRoutes.Items))
		for i, grpcRoute := range grpcRoutes.Items {
			indexRequests[i] = reconcile.Request{
				NamespacedName: client.ObjectKey{
					Namespace: grpcRoute.Namespace,
					Name:      grpcRoute.Name,
				},
			}
		}

		// Add requests for Plugins referencing the GRPCRoute via annotation.
		requests := MapRouteForKongResource[*configurationv1.KongPlugin](kindGRPCRoute)(ctx, obj)
		return append(requests, indexRequests...)
	}
}
