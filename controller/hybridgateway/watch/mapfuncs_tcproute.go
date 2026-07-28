package watch

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// listTCPRoutesForGateway returns all reconcile.Requests for TCPRoutes referencing the given Gateway.
func listTCPRoutesForGateway(ctx context.Context, cl client.Client, gatewayNamespace, gatewayName string) ([]reconcile.Request, error) {
	tcpRoutes := &gwtypes.TCPRouteList{}
	err := cl.List(ctx, tcpRoutes, client.MatchingFields{
		index.GatewayOnTCPRouteIndex: gatewayNamespace + "/" + gatewayName,
	})
	if err != nil {
		return nil, err
	}
	requests := make([]reconcile.Request, len(tcpRoutes.Items))
	for i, tcpRoute := range tcpRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: tcpRoute.Namespace,
				Name:      tcpRoute.Name,
			},
		}
	}
	return requests, nil
}

// listTCPRoutesForService returns all reconcile.Requests for TCPRoutes referencing the given service as the backend.
func listTCPRoutesForService(ctx context.Context, cl client.Client, svcNamespace, svcName string) ([]reconcile.Request, error) {
	tcpRoutes := &gwtypes.TCPRouteList{}

	err := cl.List(ctx, tcpRoutes, client.MatchingFields{
		index.BackendServicesOnTCPRouteIndex: svcNamespace + "/" + svcName,
	})
	if err != nil {
		return nil, err
	}

	requests := make([]reconcile.Request, len(tcpRoutes.Items))
	for i, tcpRoute := range tcpRoutes.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: tcpRoute.Namespace,
				Name:      tcpRoute.Name,
			},
		}
	}
	return requests, nil
}

// MapTCPRouteForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// finds all TCPRoute in the "from" namespaces that have cross-namespace backend references
// to the ReferenceGrant's namespace. It returns a slice of reconcile.Requests for each matching
// TCPRoute, enabling efficient event handling and reconciliation when a ReferenceGrant changes.
func MapTCPRouteForReferenceGrant(cl client.Client) handler.MapFunc {
	return MapRouteForReferenceGrant[gwtypes.TCPRouteList](cl)
}
