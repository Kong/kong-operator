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

func (c *tcpRouteConverter) winningTCPRoutePortsByParentRef(
	ctx context.Context,
	logger logr.Logger,
	supportedParentRefs []hybridGatewayParent,
) (map[l4ParentRefKey][]int32, error) {
	tcpRoutes, err := c.listTCPRouteCandidates(ctx)
	if err != nil {
		return nil, err
	}

	parentRefs := make([]gwtypes.ParentReference, 0, len(supportedParentRefs))
	for _, parent := range supportedParentRefs {
		parentRefs = append(parentRefs, parent.parentRef)
	}

	return winningL4RoutePortsByParentRef(
		c.route,
		tcpRoutes,
		parentRefs,
		func(tcpRoute *gwtypes.TCPRoute) ([]l4RouteAttachment, error) {
			return c.tcpRouteListenerAttachments(ctx, logger, tcpRoute)
		},
	)
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
) ([]l4RouteAttachment, error) {
	attachments := make([]l4RouteAttachment, 0)
	for i := range tcpRoute.Spec.ParentRefs {
		parentRef := &tcpRoute.Spec.ParentRefs[i]
		parentRefAttachments, err := c.tcpRouteListenerAttachmentsForParentRef(ctx, logger, tcpRoute, parentRef)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, parentRefAttachments...)
	}
	return attachments, nil
}

func (c *tcpRouteConverter) tcpRouteListenerAttachmentsForParentRef(
	ctx context.Context,
	logger logr.Logger,
	tcpRoute *gwtypes.TCPRoute,
	parentRef *gwtypes.ParentReference,
) ([]l4RouteAttachment, error) {
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

	attachments := make([]l4RouteAttachment, 0, len(listeners))
	parentRefKey := l4ParentRefKeyForRoute(tcpRoute, parentRef)
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

func sameTCPRoute(lhs, rhs *gwtypes.TCPRoute) bool {
	return lhs != nil &&
		rhs != nil &&
		lhs.Namespace == rhs.Namespace &&
		lhs.Name == rhs.Name
}

func isExpectedParentRefError(err error) bool {
	return errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind) ||
		errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup) ||
		errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound)
}
