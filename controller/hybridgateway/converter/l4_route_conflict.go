package converter

import (
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
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
