package watch

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/fullhybrid/refs"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
)

// FilterBy returns a predicate function for filtering client.Objects based on the type of the provided obj.
func FilterBy(cl client.Client, obj client.Object) (func(obj client.Object) bool, error) {
	switch o := obj.(type) {
	case *corev1.Service:
		return filterByService(cl), nil
	default:
		return nil, fmt.Errorf("unsupported object type during creation of predicates: %T", o)
	}
}

func filterByService(cl client.Client) func(obj client.Object) bool {
	return func(obj client.Object) bool {
		service, ok := obj.(*corev1.Service)
		if !ok {
			// In case of an error, enqueue the event and in case the error persists
			// the reconciler will log it and act accordingly.
			return true
		}
		ctx := context.Background()
		httpRoutes := &gwtypes.HTTPRouteList{}
		err := cl.List(ctx, httpRoutes,
			client.InNamespace(service.Namespace),
			client.MatchingFields{
				index.BackendServicesOnHTTPRouteIndex: service.Namespace + "/" + service.Name,
			},
		)
		if err != nil {
			return true
		}

		for _, httpRoute := range httpRoutes.Items {
			konnectGatewayControlPlaneRefs, err := refs.GetNamespacedRefs(ctx, cl, &httpRoute)
			if err != nil {
				// In case of an error, enqueue the event and in case the error persists
				// the reconciler will log it and act accordingly.
				return true
			}
			// in case the HTTPRoute needs to be configured in Konnect a Konnect Gateway ControlPlane, we filter the service in
			if len(konnectGatewayControlPlaneRefs) > 0 {
				return true
			}
		}
		return false
	}
}
