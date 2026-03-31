package watch

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// MapTLSRouteForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// finds all TLSRoute in the "from" namespaces that have cross-namespace backend references
// to the ReferenceGrant's namespace. It returns a slice of reconcile.Requests for each matching
// TLSRoute, enabling efficient event handling and reconciliation when a ReferenceGrant changes.
func MapTLSRouteForReferenceGrant(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		rg, ok := obj.(*gwtypes.ReferenceGrant)
		if !ok {
			return nil
		}

		// For each from namespace in the ReferenceGrant, list all TLSRoutes
		// that have cross-namespace backend refs to the ReferenceGrant's namespace.
		var requests []reconcile.Request
		for _, from := range rg.Spec.From {
			// Check that the from kind is HTTPRoute and group is gateway.networking.k8s.io.
			if from.Kind != "TLSRoute" || (from.Group != "" && from.Group != gwtypes.GroupName) {
				continue
			}

			tlsRoutes := &gwtypes.TLSRouteList{}
			err := cl.List(ctx, tlsRoutes, client.InNamespace(string(from.Namespace)))
			if err != nil {
				return nil
			}

			for _, tlsRoute := range tlsRoutes.Items {
				// Check if the TLSRoute has any backend refs to the ReferenceGrant's namespace.
				hasCrossNamespaceRef := false
				for _, rule := range tlsRoute.Spec.Rules {
					for _, backendRef := range rule.BackendRefs {
						// The backend must be in the ReferenceGrant's namespace (target namespace).
						if backendRef.Namespace != nil && string(*backendRef.Namespace) == rg.Namespace && tlsRoute.Namespace != rg.Namespace {
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
							Namespace: tlsRoute.Namespace,
							Name:      tlsRoute.Name,
						},
					})
				}
			}
		}
		return requests
	}
}
