package converter

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	hybridgatewayroute "github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const udpRouteKind = "UDPRoute"

func (c *udpRouteConverter) winningUDPRoutePortsByParentRef(
	ctx context.Context,
	logger logr.Logger,
	supportedParentRefs []hybridGatewayParent,
) (map[l4ParentRefKey][]int32, error) {
	udpRoutes, err := c.listUDPRouteCandidates(ctx)
	if err != nil {
		return nil, err
	}

	parentRefs := make([]gwtypes.ParentReference, 0, len(supportedParentRefs))
	for _, parent := range supportedParentRefs {
		parentRefs = append(parentRefs, parent.parentRef)
	}

	return winningL4RoutePortsByParentRef(
		c.route,
		udpRoutes,
		parentRefs,
		func(udpRoute *gwtypes.UDPRoute) ([]l4RouteAttachment, error) {
			return c.udpRouteListenerAttachments(ctx, logger, udpRoute)
		},
	)
}

func (c *udpRouteConverter) listUDPRouteCandidates(ctx context.Context) ([]*gwtypes.UDPRoute, error) {
	var udpRouteList gwtypes.UDPRouteList
	if err := c.List(ctx, &udpRouteList); err != nil {
		return nil, fmt.Errorf("failed to list UDPRoutes for conflict resolution: %w", err)
	}

	udpRoutes := make([]*gwtypes.UDPRoute, 0, len(udpRouteList.Items)+1)
	currentRouteListed := false
	for i := range udpRouteList.Items {
		udpRoute := &udpRouteList.Items[i]
		if sameUDPRoute(udpRoute, c.route) {
			currentRouteListed = true
		}
		udpRoutes = append(udpRoutes, udpRoute)
	}

	if !currentRouteListed {
		udpRoutes = append(udpRoutes, c.route)
	}

	return udpRoutes, nil
}

func (c *udpRouteConverter) udpRouteListenerAttachments(
	ctx context.Context,
	logger logr.Logger,
	udpRoute *gwtypes.UDPRoute,
) ([]l4RouteAttachment, error) {
	attachments := make([]l4RouteAttachment, 0)
	for i := range udpRoute.Spec.ParentRefs {
		parentRef := &udpRoute.Spec.ParentRefs[i]
		parentRefAttachments, err := c.udpRouteListenerAttachmentsForParentRef(ctx, logger, udpRoute, parentRef)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, parentRefAttachments...)
	}
	return attachments, nil
}

func (c *udpRouteConverter) udpRouteListenerAttachmentsForParentRef(
	ctx context.Context,
	logger logr.Logger,
	udpRoute *gwtypes.UDPRoute,
	parentRef *gwtypes.ParentReference,
) ([]l4RouteAttachment, error) {
	gateway, found, err := refs.GetSupportedGatewayForParentRef(ctx, logger, c.Client, *parentRef, udpRoute.Namespace)
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
		udpRouteKind,
		*parentRef,
		gateway.Spec.Listeners,
	)
	if condition != nil {
		return nil, nil
	}

	attachments := make([]l4RouteAttachment, 0, len(listeners))
	parentRefKey := l4ParentRefKeyForRoute(udpRoute, parentRef)
	for _, listener := range listeners {
		if listener.Protocol != gwtypes.UDPProtocolType {
			continue
		}
		allowed, err := c.udpListenerAllowsRoute(ctx, gateway, listener, udpRoute)
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

func (c *udpRouteConverter) udpListenerAllowsRoute(
	ctx context.Context,
	gateway *gwtypes.Gateway,
	listener gatewayv1.Listener,
	udpRoute *gwtypes.UDPRoute,
) (bool, error) {
	if listener.AllowedRoutes == nil {
		return true, nil
	}

	allowedRoutes := listener.AllowedRoutes
	if len(allowedRoutes.Kinds) > 0 {
		kindAllowed := slices.ContainsFunc(allowedRoutes.Kinds, routeGroupKindMatchesUDPRoute)
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
		return gateway.Namespace == udpRoute.Namespace, nil
	case gatewayv1.NamespacesFromSelector:
		if allowedRoutes.Namespaces.Selector == nil {
			return false, nil
		}
		var namespace corev1.Namespace
		if err := c.Get(ctx, types.NamespacedName{Name: udpRoute.Namespace}, &namespace); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to get namespace %s for UDPRoute conflict resolution: %w", udpRoute.Namespace, err)
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

func routeGroupKindMatchesUDPRoute(kind gatewayv1.RouteGroupKind) bool {
	if kind.Kind != gatewayv1.Kind(udpRouteKind) {
		return false
	}
	return kind.Group == nil || *kind.Group == gatewayv1.GroupName
}

func sameUDPRoute(lhs, rhs *gwtypes.UDPRoute) bool {
	return lhs != nil &&
		rhs != nil &&
		lhs.Namespace == rhs.Namespace &&
		lhs.Name == rhs.Name
}
