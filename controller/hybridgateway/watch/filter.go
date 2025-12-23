package watch

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kong/kong-operator/controller/hybridgateway/refs"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// FilterBy returns a predicate function for filtering client.Objects based on the type of the provided obj.
func FilterBy(ctx context.Context, cl client.Client, obj client.Object) (*predicate.Funcs, error) {
	switch o := obj.(type) {
	case *gwtypes.Gateway:
		return filterByGateway(ctx, cl), nil
	case *gwtypes.HTTPRoute:
		return filterByHTTPRoute(ctx, cl), nil
	default:
		return nil, fmt.Errorf("unsupported object type during creation of predicates: %T", o)
	}
}

// filterByHTTPRoute returns a predicate.Funcs that filters HTTPRoute objects
// based on whether they reference a Konnect Gateway ControlPlane.
func filterByHTTPRoute(ctx context.Context, cl client.Client) *predicate.Funcs {
	filter := func(obj client.Object) bool {
		httpRoute, ok := obj.(*gwtypes.HTTPRoute)
		if !ok {
			// In case of an error, enqueue the event and in case the error persists
			// the reconciler will log it and act accordingly.
			return true
		}

		konnectGatewayControlPlaneRefs, err := refs.GetNamespacedRefs(ctx, cl, httpRoute)
		if err != nil {
			// In case of an error, enqueue the event and in case the error persists
			// the reconciler will log it and act accordingly.
			return true
		}
		// in case the HTTPRoute needs to be configured in Konnect a Konnect Gateway ControlPlane should exist.
		if len(konnectGatewayControlPlaneRefs) > 0 {
			return true
		}
		return false
	}

	return &predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return filter(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// If either the old or new object passes the filter, we want to reconcile.
			// This ensures we handle cases where the object starts or stops matching the filter criteria.
			if filter(e.ObjectNew) {
				return true
			}
			return filter(e.ObjectOld)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return filter(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return filter(e.Object)
		},
	}
}

// filterByGateway returns a predicate.Funcs that filters Gateway objects
// based on whether they are supported by this controller.
func filterByGateway(ctx context.Context, cl client.Client) *predicate.Funcs {
	filter := func(obj client.Object) bool {
		gateway, ok := obj.(*gwtypes.Gateway)
		if !ok {
			// In case of an error, enqueue the event and in case the error persists
			// the reconciler will log it and act accordingly.
			return true
		}

		// Check if the Gateway is supported (i.e., controlled by us).
		supported, err := refs.IsGatewayInKonnect(ctx, cl, gateway)
		if err != nil {
			// For other errors (e.g., temporary API server issues), enqueue the event
			// so the reconciler can handle it and log it if the error persists.
			return true
		}

		return supported
	}

	return &predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return filter(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// If either the old or new object passes the filter, we want to reconcile.
			// This ensures we handle cases where the object starts or stops matching the filter criteria.
			if filter(e.ObjectNew) {
				return true
			}
			return filter(e.ObjectOld)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return filter(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return filter(e.Object)
		},
	}
}
