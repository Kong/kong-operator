package gateway

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kong/kong-operator/ingress-controller/internal/controllers"
	"github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
)

func IsRouteAttachedToReconciledGatewayPredicate[routeT gatewayapi.RouteT](
	cl client.Client,
	logger logr.Logger,
	gatewayNN controllers.OptionalNamespacedName,
) predicate.Predicate {
	return predicate.Funcs{
		GenericFunc: func(_ event.GenericEvent) bool {
			return false // we don't need to enqueue from generic
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return IsRouteAttachedToReconciledGateway[routeT](cl, logger, gatewayNN, e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isOrWasRouteAttachedToReconciledGateway[routeT](cl, logger, gatewayNN, e)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return IsRouteAttachedToReconciledGateway[routeT](cl, logger, gatewayNN, e.Object)
		},
	}
}

func IsRouteAttachedToReconciledGateway[routeT gatewayapi.RouteT](
	cl client.Client, log logr.Logger, gatewayNN controllers.OptionalNamespacedName, obj client.Object,
) bool {
	route, ok := obj.(routeT)
	if !ok {
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		log.Error(
			fmt.Errorf("unexpected object type"),
			"Route watch predicate received unexpected object type",
			"expected", kind, "found", reflect.TypeOf(obj),
		)
		return false
	}

	parentRefs := getRouteParentRefs(route)

	// If the reconciler has a GatewayNN set, only HTTPRoutes attached to that Gateway are reconciled.
	if gNN, ok := gatewayNN.Get(); ok {
		for _, parentRef := range parentRefs {
			if parentRef.Namespace != nil && string(*parentRef.Namespace) != gNN.Namespace {
				continue
			}
			if string(parentRef.Name) != gNN.Name {
				continue
			}
			if parentRef.Kind != nil && *parentRef.Kind != "Gateway" {
				continue
			}
			if parentRef.Group != nil && *parentRef.Group != gatewayapi.Group(gatewayapi.GroupVersion.Group) {
				continue
			}
			return true
		}
		return false
	}

	// If the GatewayNN is not set, all HTTPRoutes are reconciled.
	// Hence we need to check if the HTTPRoute is attached to a Gateway that is managed by this controller.
	for _, parentRef := range parentRefs {
		namespace := route.GetNamespace()
		if parentRef.Namespace != nil {
			namespace = string(*parentRef.Namespace)
		}

		kind := gatewayapi.Kind("Gateway")
		if parentRef.Kind != nil {
			kind = *parentRef.Kind
		}

		group := gatewayapi.GroupVersion.Group
		if parentRef.Group != nil {
			group = string(*parentRef.Group)
		}
		// Check the parent gateway if the parentRef points to a gateway that is possible to be controlled by KIC.
		if kind == "Gateway" && group == gatewayapi.GroupVersion.Group {
			var gateway gatewayapi.Gateway
			err := cl.Get(context.Background(), k8stypes.NamespacedName{Namespace: namespace, Name: string(parentRef.Name)}, &gateway)
			if err != nil {
				log.Error(err, "Failed to get Gateway in HTTPRoute watch")
				return false
			}

			var gatewayClass gatewayapi.GatewayClass
			err = cl.Get(context.Background(), k8stypes.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, &gatewayClass)
			if err != nil {
				log.Error(err, "Failed to get GatewayClass in HTTPRoute watch")
				return false
			}

			if isGatewayClassControlled(&gatewayClass) {
				return true
			}
		}
	}

	return false
}

func isOrWasRouteAttachedToReconciledGateway[routeT gatewayapi.RouteT](
	cl client.Client, log logr.Logger, gatewayNN controllers.OptionalNamespacedName, e event.UpdateEvent,
) bool {
	oldObj, newObj := e.ObjectOld, e.ObjectNew
	return IsRouteAttachedToReconciledGateway[routeT](cl, log, gatewayNN, oldObj) ||
		IsRouteAttachedToReconciledGateway[routeT](cl, log, gatewayNN, newObj)
}
