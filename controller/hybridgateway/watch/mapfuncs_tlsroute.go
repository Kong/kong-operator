package watch

import (
	"context"

	corev1 "k8s.io/api/core/v1"
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

func listTLSRoutesForService(ctx context.Context, cl client.Client, svcNamespace, svcName string) ([]reconcile.Request, error) {
	tlsRoutes := &gwtypes.TLSRouteList{}

	// List all HTTPRoutes that reference this Service using the index.
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
func MapTLSRouteForGateway(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gateway, ok := obj.(*gwtypes.Gateway)
		if !ok {
			return nil
		}
		requests, err := listTLSRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
		if err != nil {
			return nil
		}
		return requests
	}
}

func MapTLSRouteForService(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return nil
		}

		requests, err := listTLSRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		if err != nil {
			return nil
		}
		return requests
	}
}
