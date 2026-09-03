package watch

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// listUDPRoutesForGateway returns all reconcile.Requests for UDPRoutes referencing the given Gateway.
func listUDPRoutesForGateway(ctx context.Context, cl client.Client, gatewayNamespace, gatewayName string) ([]reconcile.Request, error) {
	udpRoutes := &gwtypes.UDPRouteList{}
	err := cl.List(ctx, udpRoutes, client.MatchingFields{
		index.GatewayOnUDPRouteIndex: gatewayNamespace + "/" + gatewayName,
	})
	if err != nil {
		return nil, err
	}
	requests := make([]reconcile.Request, len(udpRoutes.Items))
	for i, udpRoute := range udpRoutes.Items {
		requests[i] = reconcile.Request{
			Namespace: udpRoute.Namespace,
			Name:      udpRoute.Name,
		}
	}
	return requests, nil
}

// listUDPRoutesForService returns all reconcile.Requests for UDPRoutes referencing the given service as the backend.
func listUDPRoutesForService(ctx context.Context, cl client.Client, svcNamespace, svcName string) ([]reconcile.Request, error) {
	udpRoutes := &gwtypes.UDPRouteList{}

	err := cl.List(ctx, udpRoutes, client.MatchingFields{
		index.BackendServicesOnUDPRouteIndex: svcNamespace + "/" + svcName,
	})
	if err != nil {
		return nil, err
	}

	requests := make([]reconcile.Request, len(udpRoutes.Items))
	for i, udpRoute := range udpRoutes.Items {
		requests[i] = reconcile.Request{
			Namespace: udpRoute.Namespace,
			Name:      udpRoute.Name,
		}
	}
	return requests, nil
}

// MapUDPRouteForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// finds all UDPRoute in the "from" namespaces that have cross-namespace backend references
// to the ReferenceGrant's namespace. It returns a slice of reconcile.Requests for each matching
// UDPRoute, enabling efficient event handling and reconciliation when a ReferenceGrant changes.
func MapUDPRouteForReferenceGrant(cl client.Client) handler.MapFunc {
	return MapRouteForReferenceGrant[gwtypes.UDPRouteList](cl)
}
