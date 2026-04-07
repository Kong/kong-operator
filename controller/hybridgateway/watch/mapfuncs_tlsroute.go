package watch

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// listTLSRoutesForGateway returns all reconcile.Requests for TLSRoutes referencing the given Gateway.
func listTLSRoutesForGateway(ctx context.Context, cl client.Client, gatewayNamespace, gatewayName string) ([]reconcile.Request, error) {
	tlsRoutes := &gwtypes.TLSRouteList{}
	err := cl.List(ctx, tlsRoutes, client.MatchingFields{
		index.GatewayOnTLSRouteIndex: gatewayNamespace + "/" + gatewayName,
	})
	if err != nil {
		return nil, err
	}
	requests := make([]reconcile.Request, len(tlsRoutes.Items))
	for i, tlsRoute := range tlsRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: tlsRoute.Namespace,
				Name:      tlsRoute.Name,
			},
		}
	}
	return requests, nil
}

// listTLSRoutesForService returns all reconcile.Requests for TLSRoutes referencing the given service as the backend.
func listTLSRoutesForService(ctx context.Context, cl client.Client, svcNamespace, svcName string) ([]reconcile.Request, error) {
	tlsRoutes := &gwtypes.TLSRouteList{}

	// List all TLSRoutes that reference this Service using the index.
	err := cl.List(ctx, tlsRoutes, client.MatchingFields{
		index.BackendServicesOnTLSRouteIndex: svcNamespace + "/" + svcName,
	})
	if err != nil {
		return nil, err
	}

	requests := make([]reconcile.Request, len(tlsRoutes.Items))
	for i, tlsRoute := range tlsRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: tlsRoute.Namespace,
				Name:      tlsRoute.Name,
			},
		}
	}
	return requests, nil
}

// MapTLSRouteForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// finds all TLSRoute in the "from" namespaces that have cross-namespace backend references
// to the ReferenceGrant's namespace. It returns a slice of reconcile.Requests for each matching
// TLSRoute, enabling efficient event handling and reconciliation when a ReferenceGrant changes.
func MapTLSRouteForReferenceGrant(cl client.Client) handler.MapFunc {
	return MapRouteForReferenceGrant[gwtypes.TLSRouteList](cl)
}
