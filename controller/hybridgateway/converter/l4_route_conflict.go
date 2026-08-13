package converter

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	hybridgatewayroute "github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const (
	tcpRouteKind = "TCPRoute"
	udpRouteKind = "UDPRoute"
)

// l4Route constrains layer-4 Gateway API route types that share listener-level
// oldest-route-wins conflict arbitration.
type l4Route interface {
	client.Object
	*gwtypes.TCPRoute | *gwtypes.UDPRoute
}

type l4ListenerKey struct {
	gatewayNamespace string
	gatewayName      string
	listenerName     string
	port             gatewayv1.PortNumber
}

type l4ParentRefKey struct {
	namespace   string
	name        string
	sectionName string
	port        int32
}

type l4RouteAttachment struct {
	listenerKey  l4ListenerKey
	parentRefKey l4ParentRefKey
}

type l4RouteAttachmentsFunc[T l4Route] func(T) ([]l4RouteAttachment, error)

// l4RouteListerFunc lists all live candidates of a layer-4 route type, for listener-level
// conflict arbitration.
type l4RouteListerFunc[T l4Route] func(ctx context.Context) ([]T, error)

// winningL4RoutePortsForRoute lists every live candidate of route's kind (via list), ensures route
// itself is among them even if not yet persisted, and returns the listener ports route won
// oldest-route-wins arbitration for, keyed by supported ParentRef.
func winningL4RoutePortsForRoute[T l4Route](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	route T,
	routeKind string,
	protocol gatewayv1.ProtocolType,
	list l4RouteListerFunc[T],
	supportedParentRefs []hybridGatewayParent,
) (map[l4ParentRefKey][]int32, error) {
	candidates, err := list(ctx)
	if err != nil {
		return nil, err
	}

	currentRouteListed := false
	for _, candidate := range candidates {
		if sameL4Route(candidate, route) {
			currentRouteListed = true
			break
		}
	}
	if !currentRouteListed {
		candidates = append(candidates, route)
	}

	parentRefs := make([]gwtypes.ParentReference, 0, len(supportedParentRefs))
	for _, parent := range supportedParentRefs {
		parentRefs = append(parentRefs, parent.parentRef)
	}

	return winningL4RoutePortsByParentRef(
		route,
		candidates,
		parentRefs,
		func(candidate T) ([]l4RouteAttachment, error) {
			return l4RouteListenerAttachments(ctx, logger, cl, candidate, routeKind, protocol)
		},
	)
}

func winningL4RoutePortsByParentRef[T l4Route](
	route T,
	candidates []T,
	parentRefs []gwtypes.ParentReference,
	attachmentsForRoute l4RouteAttachmentsFunc[T],
) (map[l4ParentRefKey][]int32, error) {
	listenerAttachments := make(map[l4ListenerKey][]T)
	currentRouteAttachments := make([]l4RouteAttachment, 0)
	for _, candidate := range candidates {
		attachments, err := attachmentsForRoute(candidate)
		if err != nil {
			return nil, err
		}
		for _, attachment := range attachments {
			listenerAttachments[attachment.listenerKey] = append(listenerAttachments[attachment.listenerKey], candidate)
		}
		if sameL4Route(candidate, route) {
			currentRouteAttachments = append(currentRouteAttachments, attachments...)
		}
	}

	portsByParentRef := make(map[l4ParentRefKey][]int32, len(parentRefs))
	supportedParentRefs := make(map[l4ParentRefKey]struct{}, len(parentRefs))
	for i := range parentRefs {
		parentRefKey := l4ParentRefKeyForRoute(route, &parentRefs[i])
		portsByParentRef[parentRefKey] = nil
		supportedParentRefs[parentRefKey] = struct{}{}
	}

	for _, attachment := range currentRouteAttachments {
		if _, ok := supportedParentRefs[attachment.parentRefKey]; !ok {
			continue
		}
		winner, ok := pickWinningL4Route(listenerAttachments[attachment.listenerKey])
		if !ok || !sameL4Route(winner, route) {
			continue
		}
		portsByParentRef[attachment.parentRefKey] = append(
			portsByParentRef[attachment.parentRefKey],
			attachment.listenerKey.port,
		)
	}

	for parentRefKey, ports := range portsByParentRef {
		portsByParentRef[parentRefKey] = deduplicateL4Ports(ports)
	}

	return portsByParentRef, nil
}

// l4RouteListenerAttachments returns, for each of route's ParentRefs, the Gateway listeners it
// attaches to.
func l4RouteListenerAttachments[T l4Route](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	route T,
	routeKind string,
	protocol gatewayv1.ProtocolType,
) ([]l4RouteAttachment, error) {
	attachments := make([]l4RouteAttachment, 0)
	for _, parentRef := range l4RouteParentRefs(route) {
		parentRefAttachments, err := l4RouteListenerAttachmentsForParentRef(ctx, logger, cl, route, &parentRef, routeKind, protocol)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, parentRefAttachments...)
	}
	return attachments, nil
}

func l4RouteListenerAttachmentsForParentRef[T l4Route](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	route T,
	parentRef *gwtypes.ParentReference,
	routeKind string,
	protocol gatewayv1.ProtocolType,
) ([]l4RouteAttachment, error) {
	gateway, found, err := refs.GetSupportedGatewayForParentRef(ctx, logger, cl, *parentRef, route.GetNamespace())
	if err != nil {
		if isExpectedParentRefError(err) {
			return nil, nil
		}
		return nil, err
	}
	if !found {
		return nil, nil
	}

	listeners, condition := hybridgatewayroute.FilterMatchingListeners(
		logger,
		gateway,
		routeKind,
		*parentRef,
		gateway.Spec.Listeners,
	)
	if condition != nil {
		return nil, nil
	}

	attachments := make([]l4RouteAttachment, 0, len(listeners))
	parentRefKey := l4ParentRefKeyForRoute(route, parentRef)
	for _, listener := range listeners {
		if listener.Protocol != protocol {
			continue
		}
		allowed, err := l4ListenerAllowsRoute(ctx, cl, gateway, listener, route, routeKind)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}

		attachments = append(attachments, l4RouteAttachment{
			listenerKey: l4ListenerKey{
				gatewayNamespace: gateway.Namespace,
				gatewayName:      gateway.Name,
				listenerName:     string(listener.Name),
				port:             listener.Port,
			},
			parentRefKey: parentRefKey,
		})
	}

	return attachments, nil
}

func l4ListenerAllowsRoute[T l4Route](
	ctx context.Context,
	cl client.Client,
	gateway *gwtypes.Gateway,
	listener gatewayv1.Listener,
	route T,
	routeKind string,
) (bool, error) {
	if listener.AllowedRoutes == nil {
		return true, nil
	}

	allowedRoutes := listener.AllowedRoutes
	if len(allowedRoutes.Kinds) > 0 {
		kindAllowed := slices.ContainsFunc(allowedRoutes.Kinds, func(kind gatewayv1.RouteGroupKind) bool {
			return routeGroupKindMatchesL4RouteKind(kind, routeKind)
		})
		if !kindAllowed {
			return false, nil
		}
	}

	if allowedRoutes.Namespaces == nil || allowedRoutes.Namespaces.From == nil {
		return true, nil
	}

	switch *allowedRoutes.Namespaces.From {
	case gatewayv1.NamespacesFromAll:
		return true, nil
	case gatewayv1.NamespacesFromSame:
		return gateway.Namespace == route.GetNamespace(), nil
	case gatewayv1.NamespacesFromSelector:
		if allowedRoutes.Namespaces.Selector == nil {
			return false, nil
		}
		var namespace corev1.Namespace
		if err := cl.Get(ctx, types.NamespacedName{Name: route.GetNamespace()}, &namespace); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to get namespace %s for %s conflict resolution: %w", route.GetNamespace(), routeKind, err)
		}
		selector, err := metav1.LabelSelectorAsSelector(allowedRoutes.Namespaces.Selector)
		if err != nil {
			return false, fmt.Errorf("invalid namespace selector for Gateway %s/%s listener %s: %w",
				gateway.Namespace, gateway.Name, listener.Name, err)
		}
		return selector.Matches(labels.Set(namespace.Labels)), nil
	default:
		return false, fmt.Errorf("unknown value for AllowedRoutes.Namespaces.From: %s for listener %s for gateway %s/%s",
			*allowedRoutes.Namespaces.From, listener.Name, gateway.Namespace, gateway.Name)
	}
}

func routeGroupKindMatchesL4RouteKind(kind gatewayv1.RouteGroupKind, routeKind string) bool {
	if kind.Kind != gatewayv1.Kind(routeKind) {
		return false
	}
	return kind.Group == nil || *kind.Group == gatewayv1.GroupName
}

func isExpectedParentRefError(err error) bool {
	return errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind) ||
		errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup) ||
		errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound)
}

// l4RouteParentRefs returns the ParentRefs of a layer-4 route.
func l4RouteParentRefs[T l4Route](route T) []gwtypes.ParentReference {
	switch r := any(route).(type) {
	case *gwtypes.TCPRoute:
		return r.Spec.ParentRefs
	case *gwtypes.UDPRoute:
		return r.Spec.ParentRefs
	default:
		return nil
	}
}

func pickWinningL4Route[T l4Route](routes []T) (T, bool) {
	var zero T
	if len(routes) == 0 {
		return zero, false
	}

	winner := routes[0]
	for _, route := range routes[1:] {
		if l4RouteLess(route, winner) {
			winner = route
		}
	}
	return winner, true
}

func l4RouteLess[T l4Route](lhs, rhs T) bool {
	lhsTS, rhsTS := lhs.GetCreationTimestamp(), rhs.GetCreationTimestamp()
	if lhsTS.Before(&rhsTS) {
		return true
	}
	if rhsTS.Before(&lhsTS) {
		return false
	}
	if lhs.GetNamespace() != rhs.GetNamespace() {
		return lhs.GetNamespace() < rhs.GetNamespace()
	}
	return lhs.GetName() < rhs.GetName()
}

func sameL4Route[T l4Route](lhs, rhs T) bool {
	return lhs.GetNamespace() == rhs.GetNamespace() &&
		lhs.GetName() == rhs.GetName()
}

func l4ParentRefKeyForRoute[T l4Route](route T, parentRef *gwtypes.ParentReference) l4ParentRefKey {
	namespace := route.GetNamespace()
	if parentRef.Namespace != nil && *parentRef.Namespace != "" {
		namespace = string(*parentRef.Namespace)
	}

	sectionName := ""
	if parentRef.SectionName != nil {
		sectionName = string(*parentRef.SectionName)
	}

	var port int32
	if parentRef.Port != nil {
		port = *parentRef.Port
	}

	return l4ParentRefKey{
		namespace:   namespace,
		name:        string(parentRef.Name),
		sectionName: sectionName,
		port:        port,
	}
}

func deduplicateL4Ports(ports []int32) []int32 {
	if len(ports) == 0 {
		return nil
	}

	slices.Sort(ports)
	ports = slices.Compact(ports)
	return ports
}

func (c *tcpRouteConverter) winningTCPRoutePortsByParentRef(
	ctx context.Context,
	logger logr.Logger,
	supportedParentRefs []hybridGatewayParent,
) (map[l4ParentRefKey][]int32, error) {
	return winningL4RoutePortsForRoute(
		ctx, logger, c.Client, c.route, tcpRouteKind, gwtypes.TCPProtocolType,
		func(ctx context.Context) ([]*gwtypes.TCPRoute, error) {
			var tcpRouteList gwtypes.TCPRouteList
			if err := c.List(ctx, &tcpRouteList); err != nil {
				return nil, fmt.Errorf("failed to list TCPRoutes for conflict resolution: %w", err)
			}
			routes := make([]*gwtypes.TCPRoute, 0, len(tcpRouteList.Items))
			for i := range tcpRouteList.Items {
				routes = append(routes, &tcpRouteList.Items[i])
			}
			return routes, nil
		},
		supportedParentRefs,
	)
}

func (c *udpRouteConverter) winningUDPRoutePortsByParentRef(
	ctx context.Context,
	logger logr.Logger,
	supportedParentRefs []hybridGatewayParent,
) (map[l4ParentRefKey][]int32, error) {
	return winningL4RoutePortsForRoute(
		ctx, logger, c.Client, c.route, udpRouteKind, gwtypes.UDPProtocolType,
		func(ctx context.Context) ([]*gwtypes.UDPRoute, error) {
			var udpRouteList gwtypes.UDPRouteList
			if err := c.List(ctx, &udpRouteList); err != nil {
				return nil, fmt.Errorf("failed to list UDPRoutes for conflict resolution: %w", err)
			}
			routes := make([]*gwtypes.UDPRoute, 0, len(udpRouteList.Items))
			for i := range udpRouteList.Items {
				routes = append(routes, &udpRouteList.Items[i])
			}
			return routes, nil
		},
		supportedParentRefs,
	)
}
