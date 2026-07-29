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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	hybridgatewayroute "github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const tcpRouteKind = "TCPRoute"

type tcpListenerKey struct {
	gatewayNamespace string
	gatewayName      string
	listenerName     string
	port             gatewayv1.PortNumber
}

type tcpParentRefKey struct {
	namespace   string
	name        string
	sectionName string
	port        int32
}

func (c *tcpRouteConverter) winningTCPRoutePortsByParentRef(
	ctx context.Context,
	logger logr.Logger,
	supportedParentRefs []hybridGatewayParent,
) (map[tcpParentRefKey][]int32, error) {
	tcpRoutes, err := c.listTCPRouteCandidates(ctx)
	if err != nil {
		return nil, err
	}

	listenerAttachments := make(map[tcpListenerKey][]*gwtypes.TCPRoute)
	for _, tcpRoute := range tcpRoutes {
		keys, err := c.tcpRouteListenerAttachments(ctx, logger, tcpRoute)
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			listenerAttachments[key] = append(listenerAttachments[key], tcpRoute)
		}
	}

	portsByParentRef := make(map[tcpParentRefKey][]int32, len(supportedParentRefs))
	for _, parent := range supportedParentRefs {
		keys, err := c.tcpRouteListenerAttachmentsForParentRef(ctx, logger, c.route, &parent.parentRef)
		if err != nil {
			return nil, err
		}

		parentRefKey := tcpParentRefKeyForRoute(c.route, &parent.parentRef)
		ports := make([]int32, 0, len(keys))
		for _, key := range keys {
			winner := pickWinningTCPRoute(listenerAttachments[key])
			if winner == nil || !sameTCPRoute(winner, c.route) {
				continue
			}
			ports = append(ports, key.port)
		}
		portsByParentRef[parentRefKey] = deduplicateTCPPorts(ports)
	}

	return portsByParentRef, nil
}

func (c *tcpRouteConverter) listTCPRouteCandidates(ctx context.Context) ([]*gwtypes.TCPRoute, error) {
	var tcpRouteList gwtypes.TCPRouteList
	if err := c.List(ctx, &tcpRouteList); err != nil {
		return nil, fmt.Errorf("failed to list TCPRoutes for conflict resolution: %w", err)
	}

	tcpRoutes := make([]*gwtypes.TCPRoute, 0, len(tcpRouteList.Items)+1)
	currentRouteListed := false
	for i := range tcpRouteList.Items {
		tcpRoute := &tcpRouteList.Items[i]
		if sameTCPRoute(tcpRoute, c.route) {
			currentRouteListed = true
		}
		tcpRoutes = append(tcpRoutes, tcpRoute)
	}

	if !currentRouteListed {
		tcpRoutes = append(tcpRoutes, c.route)
	}

	return tcpRoutes, nil
}

func (c *tcpRouteConverter) tcpRouteListenerAttachments(
	ctx context.Context,
	logger logr.Logger,
	tcpRoute *gwtypes.TCPRoute,
) ([]tcpListenerKey, error) {
	keys := make([]tcpListenerKey, 0)
	for i := range tcpRoute.Spec.ParentRefs {
		parentRef := &tcpRoute.Spec.ParentRefs[i]
		parentRefKeys, err := c.tcpRouteListenerAttachmentsForParentRef(ctx, logger, tcpRoute, parentRef)
		if err != nil {
			return nil, err
		}
		keys = append(keys, parentRefKeys...)
	}
	return keys, nil
}

func (c *tcpRouteConverter) tcpRouteListenerAttachmentsForParentRef(
	ctx context.Context,
	logger logr.Logger,
	tcpRoute *gwtypes.TCPRoute,
	parentRef *gwtypes.ParentReference,
) ([]tcpListenerKey, error) {
	gateway, found, err := refs.GetSupportedGatewayForParentRef(ctx, logger, c.Client, *parentRef, tcpRoute.Namespace)
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
		tcpRouteKind,
		*parentRef,
		gateway.Spec.Listeners,
	)
	if condition != nil {
		return nil, nil
	}

	keys := make([]tcpListenerKey, 0, len(listeners))
	for _, listener := range listeners {
		if listener.Protocol != gwtypes.TCPProtocolType {
			continue
		}
		allowed, err := c.tcpListenerAllowsRoute(ctx, gateway, listener, tcpRoute)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}

		keys = append(keys, tcpListenerKey{
			gatewayNamespace: gateway.Namespace,
			gatewayName:      gateway.Name,
			listenerName:     string(listener.Name),
			port:             listener.Port,
		})
	}

	return keys, nil
}

func (c *tcpRouteConverter) tcpListenerAllowsRoute(
	ctx context.Context,
	gateway *gwtypes.Gateway,
	listener gatewayv1.Listener,
	tcpRoute *gwtypes.TCPRoute,
) (bool, error) {
	if listener.AllowedRoutes == nil {
		return true, nil
	}

	allowedRoutes := listener.AllowedRoutes
	if len(allowedRoutes.Kinds) > 0 {
		kindAllowed := slices.ContainsFunc(allowedRoutes.Kinds, routeGroupKindMatchesTCPRoute)
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
		return gateway.Namespace == tcpRoute.Namespace, nil
	case gatewayv1.NamespacesFromSelector:
		if allowedRoutes.Namespaces.Selector == nil {
			return false, nil
		}
		var namespace corev1.Namespace
		if err := c.Get(ctx, types.NamespacedName{Name: tcpRoute.Namespace}, &namespace); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to get namespace %s for TCPRoute conflict resolution: %w", tcpRoute.Namespace, err)
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

func routeGroupKindMatchesTCPRoute(kind gatewayv1.RouteGroupKind) bool {
	if kind.Kind != gatewayv1.Kind(tcpRouteKind) {
		return false
	}
	return kind.Group == nil || *kind.Group == gatewayv1.GroupName
}

func pickWinningTCPRoute(tcpRoutes []*gwtypes.TCPRoute) *gwtypes.TCPRoute {
	if len(tcpRoutes) == 0 {
		return nil
	}

	winner := tcpRoutes[0]
	for _, tcpRoute := range tcpRoutes[1:] {
		if tcpRouteLess(tcpRoute, winner) {
			winner = tcpRoute
		}
	}
	return winner
}

func tcpRouteLess(lhs, rhs *gwtypes.TCPRoute) bool {
	if lhs.CreationTimestamp.Before(&rhs.CreationTimestamp) {
		return true
	}
	if rhs.CreationTimestamp.Before(&lhs.CreationTimestamp) {
		return false
	}
	if lhs.Namespace != rhs.Namespace {
		return lhs.Namespace < rhs.Namespace
	}
	return lhs.Name < rhs.Name
}

func sameTCPRoute(lhs, rhs *gwtypes.TCPRoute) bool {
	return lhs != nil &&
		rhs != nil &&
		lhs.Namespace == rhs.Namespace &&
		lhs.Name == rhs.Name
}

func tcpParentRefKeyForRoute(tcpRoute *gwtypes.TCPRoute, parentRef *gwtypes.ParentReference) tcpParentRefKey {
	namespace := tcpRoute.Namespace
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

	return tcpParentRefKey{
		namespace:   namespace,
		name:        string(parentRef.Name),
		sectionName: sectionName,
		port:        port,
	}
}

func deduplicateTCPPorts(ports []int32) []int32 {
	if len(ports) == 0 {
		return nil
	}

	slices.Sort(ports)
	ports = slices.Compact(ports)
	return ports
}

func isExpectedParentRefError(err error) bool {
	return errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind) ||
		errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup) ||
		errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound)
}
