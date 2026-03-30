package watch

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// This file is for map functions shared by all supported routes in gateway APIs.

// kongResource is a type constraint that encompasses all Kong resource types
// that can be mapped back to HTTPRoutes via annotations.
type kongResource interface {
	*configurationv1alpha1.KongUpstream |
		*configurationv1alpha1.KongTarget |
		*configurationv1alpha1.KongService |
		*configurationv1alpha1.KongRoute |
		*configurationv1.KongPlugin |
		*configurationv1alpha1.KongPluginBinding
}

// MapRouteForKongResource returns a handler.MapFunc that, given a Kong resource object of type T,
// retrieves the routesreferenced in its annotations. It returns a slice of reconcile.Requests
// for each matching route.
func MapRouteForKongResource[T kongResource](cl client.Client) handler.MapFunc {
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

// MapRouteForGateway returns a handler.MapFunc that, given a Gateway object,
// lists all supported routes with the given type referencing that Gateway (via ParentRefs).
// It returns a slice of reconcile.Requests for each matching route, enabling efficient event handling
// and reconciliation when a Gateway changes.
func MapRouteForGateway[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](cl client.Client, route TPtr) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gateway, ok := obj.(*gwtypes.Gateway)
		if !ok {
			return nil
		}
		var requests []reconcile.Request
		var err error
		switch any(route).(type) {
		case *gwtypes.HTTPRoute:
			requests, err = listHTTPRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
		default:
			// Unsupported types.
			return nil
		}
		if err != nil {
			return nil
		}
		return requests
	}
}

// MapRouteForGatewayClass returns a handler.MapFunc that, given a GatewayClass object,
// lists all Gateways referencing that GatewayClass (using GatewayClassOnGatewayIndex),
// then for each Gateway, lists all routes with given type referencing it (via ParentRefs and index).
// It returns a slice of reconcile.Requests for each matching route, enabling efficient event handling
// and reconciliation when a GatewayClass changes.
func MapRouteForGatewayClass[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](cl client.Client, route TPtr) handler.MapFunc {
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
			var gwRequests []reconcile.Request
			switch any(route).(type) {
			case *gwtypes.HTTPRoute:
				gwRequests, err = listHTTPRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
			default:
				return nil
			}
			if err != nil {
				return nil
			}
			requests = append(requests, gwRequests...)
		}
		return requests
	}
}

// MapRouteForService returns a handler.MapFunc that, given a Service object,
// lists all routes with given type referencing that Service using the index for service on route.
// It returns a slice of reconcile.Requests for each matching route, enabling efficient event handling
// and reconciliation when a Service changes.
func MapRouteForService[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](cl client.Client, route TPtr) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return nil
		}

		var requests []reconcile.Request
		var err error
		switch any(route).(type) {
		case *gwtypes.HTTPRoute:
			requests, err = listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		default:
			return nil
		}

		if err != nil {
			return nil
		}
		return requests
	}
}

// MapRouteForEndpointSlice returns a handler.MapFunc that, given an EndpointSlice object,
// retrieves the owning Service and lists all routes with the given type referencing that Service using the index.
// It returns a slice of reconcile.Requests for each matching route, enabling efficient event handling
// and reconciliation when an EndpointSlice changes.
func MapRouteForEndpointSlice[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](cl client.Client, route TPtr) handler.MapFunc {
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

		var requests []reconcile.Request
		switch any(route).(type) {
		case *gwtypes.HTTPRoute:
			requests, err = listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		default:
			return nil
		}

		if err != nil {
			return nil
		}
		return requests
	}
}
