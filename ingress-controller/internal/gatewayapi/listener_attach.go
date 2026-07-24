package gatewayapi

import (
	"errors"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Sentinel errors returned by ListenerSupportsRouteInStatus and ListenerProgrammed.
var (
	ErrUnsupportedRouteKind          = errors.New("unsupported route kind")
	ErrUnmatchedListenerName         = errors.New("unmatched listener name")
	ErrListenerNoProgrammedCondition = errors.New("no Programmed condition found for listener")
	ErrListenerNotProgrammedYet      = errors.New("listener not programmed yet")
)

// routeGVK returns the GroupVersionKind for the concrete route type T.
// Typed route objects fetched from an informer cache/lister normally don't
// carry a populated TypeMeta/GVK (see
// https://github.com/kubernetes/kubernetes/issues/3030), so it's filled in
// manually here for comparison against a listener's AllowedRoutes/SupportedKinds.
func routeGVK[T RouteT](route T) schema.GroupVersionKind {
	switch any(route).(type) {
	case *HTTPRoute:
		return schema.GroupVersionKind{Group: GroupVersion.Group, Kind: "HTTPRoute"}
	case *TLSRoute:
		return schema.GroupVersionKind{Group: GroupVersion.Group, Kind: "TLSRoute"}
	case *GRPCRoute:
		return schema.GroupVersionKind{Group: GroupVersion.Group, Kind: "GRPCRoute"}
	case *UDPRoute:
		return schema.GroupVersionKind{Group: GroupVersion.Group, Kind: "UDPRoute"}
	case *TCPRoute:
		return schema.GroupVersionKind{Group: GroupVersion.Group, Kind: "TCPRoute"}
	default:
		return route.GetObjectKind().GroupVersionKind()
	}
}

// ListenerAcceptsRouteKind reports whether listener's AllowedRoutes.Kinds
// permits route's kind. An unset Kinds list allows every kind.
func ListenerAcceptsRouteKind[T RouteT](listener Listener, route T) bool {
	if listener.AllowedRoutes == nil || len(listener.AllowedRoutes.Kinds) == 0 {
		return true
	}
	gvk := routeGVK(route)
	_, ok := lo.Find(listener.AllowedRoutes.Kinds, func(rgk RouteGroupKind) bool {
		return rgk.Group != nil && string(*rgk.Group) == gvk.Group && string(rgk.Kind) == gvk.Kind
	})
	return ok
}

// ListenerAllowsNamespace evaluates listener's AllowedRoutes.Namespaces for
// the From: All and Same cases, which require no additional I/O.
//
// handled is false for From: Selector, which needs the route's Namespace
// object (for its labels) that the caller must resolve itself; ok is
// meaningless when handled is false.
func ListenerAllowsNamespace[T RouteT](
	listener Listener, route T, gatewayNamespace string, parentRefNamespace *Namespace,
) (ok bool, handled bool) {
	if listener.AllowedRoutes == nil || listener.AllowedRoutes.Namespaces == nil || listener.AllowedRoutes.Namespaces.From == nil {
		return true, true
	}

	switch *listener.AllowedRoutes.Namespaces.From {
	case NamespacesFromAll:
		return true, true

	case NamespacesFromSame:
		// If parentRef didn't specify the namespace then we check if
		// the gateway is from the same namespace as the route
		if parentRefNamespace == nil {
			return gatewayNamespace == route.GetNamespace(), true
		}
		// Otherwise compare routes namespace with parentRef's one.
		return route.GetNamespace() == string(*parentRefNamespace), true

	case NamespacesFromSelector:
		// TODO: no Namespace cache available to callers that only have a
		// store.Storer (e.g. the translator) - can't evaluate the selector
		// here. Needs a Namespace cache/lister added to that layer before
		// this can be handled like NamespacesFromSame/All.
		return false, false

	default:
		return false, true
	}
}

// ListenerSupportsRouteInStatus checks if:
// - A listener status exists with the given name.
// - The route's kind is within the supported kinds recorded in that listener's status.
func ListenerSupportsRouteInStatus[T RouteT](route T, listenerName SectionName, lss []ListenerStatus) error {
	listenerFound := false

	_, ok := lo.Find(lss, func(ls ListenerStatus) bool {
		if ls.Name != listenerName {
			return false
		}
		listenerFound = true

		gvk := routeGVK(route)
		_, ok := lo.Find(ls.SupportedKinds, func(rgk RouteGroupKind) bool {
			return rgk.Group != nil && string(*rgk.Group) == gvk.Group && string(rgk.Kind) == gvk.Kind
		})
		return ok
	})

	if !ok && !listenerFound {
		return ErrUnmatchedListenerName // Matching Listener's not found.
	}

	if !ok && listenerFound {
		return ErrUnsupportedRouteKind // Listener(s) found but none with matching supported kinds.
	}

	return nil
}

// ListenerProgrammed reports whether listenerName's Programmed condition in
// lss is True.
func ListenerProgrammed(listenerName SectionName, lss []ListenerStatus) error {
	listenerStatus, ok := lo.Find(lss, func(ls ListenerStatus) bool {
		return ls.Name == listenerName
	})
	if !ok {
		return ErrUnmatchedListenerName // Matching Listener's not found.
	}

	programmedStatus, ok := lo.Find(listenerStatus.Conditions, func(condition metav1.Condition) bool {
		return condition.Type == string(ListenerConditionProgrammed)
	})
	if !ok {
		return ErrListenerNoProgrammedCondition // "Programmed" condition not found in conditions of listener's conditions.
	}

	if programmedStatus.Status != metav1.ConditionTrue {
		return ErrListenerNotProgrammedYet // "Programmed" condition is not true.
	}

	return nil
}
