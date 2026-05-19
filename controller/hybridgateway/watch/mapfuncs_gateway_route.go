package watch

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
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

const (
	// REVIEW: define the kinds in some common packages?
	kindHTTPRoute = "HTTPRoute"
	kindTLSRoute  = "TLSRoute"
)

// kongResource is a type constraint that encompasses all Kong resource types
// that can be mapped back to routes with supported type via annotations.
type kongResource interface {
	*configurationv1alpha1.KongUpstream |
		*configurationv1alpha1.KongTarget |
		*configurationv1alpha1.KongService |
		*configurationv1alpha1.KongRoute |
		*configurationv1.KongPlugin |
		*configurationv1alpha1.KongPluginBinding |
		*configurationv1alpha1.KongCertificate |
		*configurationv1alpha1.KongReferenceGrant
}

// MapRouteForKongResource returns a handler.MapFunc that, given a Kong resource object of type T,
// retrieves the routes referenced in its annotations. It returns a slice of reconcile.Requests
// for each matching route.
func MapRouteForKongResource[T kongResource](kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		_, ok := obj.(T)
		if !ok {
			return nil
		}

		am := metadata.NewAnnotationManager(logr.Discard())
		routes := am.GetRoutesWithKind(obj, kind)
		if len(routes) == 0 {
			return nil
		}

		return lo.Map(routes, func(routeKey string, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: metadata.NameStringToObjectKey(routeKey),
			}
		})

	}
}

// MapRouteForGateway returns a handler.MapFunc that, given a Gateway object,
// lists all supported routes with the given type referencing that Gateway (via ParentRefs).
// It returns a slice of reconcile.Requests for each matching route, enabling efficient event handling
// and reconciliation when a Gateway changes.
func MapRouteForGateway[T gwtypes.SupportedRoute](cl client.Client, route T) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		gateway, ok := obj.(*gwtypes.Gateway)
		if !ok {
			return nil
		}
		var requests []reconcile.Request
		var err error
		switch any(route).(type) {
		case gwtypes.HTTPRoute:
			requests, err = listHTTPRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
		case gwtypes.TLSRoute:
			requests, err = listTLSRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
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
func MapRouteForGatewayClass[T gwtypes.SupportedRoute](cl client.Client, route T) handler.MapFunc {
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
			case gwtypes.HTTPRoute:
				gwRequests, err = listHTTPRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
			case gwtypes.TLSRoute:
				gwRequests, err = listTLSRoutesForGateway(ctx, cl, gateway.Namespace, gateway.Name)
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
func MapRouteForService[T gwtypes.SupportedRoute](cl client.Client, route T) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return nil
		}

		var requests []reconcile.Request
		var err error
		switch any(route).(type) {
		case gwtypes.HTTPRoute:
			requests, err = listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		case gwtypes.TLSRoute:
			requests, err = listTLSRoutesForService(ctx, cl, svc.Namespace, svc.Name)
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
func MapRouteForEndpointSlice[T gwtypes.SupportedRoute](cl client.Client, route T) handler.MapFunc {
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
		case gwtypes.HTTPRoute:
			requests, err = listHTTPRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		case gwtypes.TLSRoute:
			requests, err = listTLSRoutesForService(ctx, cl, svc.Namespace, svc.Name)
		default:
			return nil
		}

		if err != nil {
			return nil
		}
		return requests
	}
}

// MapRouteForReferenceGrant returns a handler.MapFunc that, given a ReferenceGrant object,
// finds all routes with the given type in the "from" namespaces that have cross-namespace backend references
// to the ReferenceGrant's namespace. It returns a slice of reconcile.Requests for each matching
// route, enabling efficient event handling and reconciliation when a ReferenceGrant changes.
func MapRouteForReferenceGrant[TList gwtypes.SupportedRouteList,
	TListPtr gwtypes.SupportedRouteListPtr[TList]](cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		rg, ok := obj.(*gwtypes.ReferenceGrant)
		if !ok {
			return nil
		}
		var kind string
		var list TList
		switch any(list).(type) {
		case gwtypes.HTTPRouteList:
			kind = "HTTPRoute"
		case gwtypes.TLSRouteList:
			kind = "TLSRoute"
		}
		var requests []reconcile.Request
		for _, from := range rg.Spec.From {
			// Check that the from kind matches the given type and group is gateway.networking.k8s.io.
			if string(from.Kind) != kind || (from.Group != "" && from.Group != gwtypes.GroupName) {
				continue
			}
			var list TList
			var listPtr TListPtr = &list
			err := cl.List(ctx, listPtr, client.InNamespace(string(from.Namespace)))
			if err != nil {
				return nil
			}

			switch l := any(listPtr).(type) {
			case *gwtypes.HTTPRouteList:
				requests = append(requests, mapRouteInListForReferenceGrant(l.Items, rg)...)
			case *gwtypes.TLSRouteList:
				requests = append(requests, mapRouteInListForReferenceGrant(l.Items, rg)...)
			}
		}
		return requests
	}
}

func mapRouteInListForReferenceGrant[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](items []T, rg *gwtypes.ReferenceGrant) []reconcile.Request {
	requests := []reconcile.Request{}
	for _, route := range items {
		var backendRefs []gwtypes.BackendRef
		var rPtr TPtr = &route

		switch r := any(route).(type) {
		case gwtypes.HTTPRoute:
			for _, rule := range r.Spec.Rules {
				for _, backendRef := range rule.BackendRefs {
					backendRefs = append(backendRefs, backendRef.BackendRef)
				}
			}
		case gwtypes.TLSRoute:
			for _, rule := range r.Spec.Rules {
				backendRefs = append(backendRefs, rule.BackendRefs...)
			}
		// TODO: Add other supported types.
		default:
			return nil
		}
		for _, backendRef := range backendRefs {
			if backendRef.Namespace != nil && string(*backendRef.Namespace) == rg.Namespace && rPtr.GetNamespace() != rg.Namespace {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKey{
						Namespace: rPtr.GetNamespace(),
						Name:      rPtr.GetName(),
					},
				})
				break
			}
		}
	}
	return requests
}
