package gateway

import (
	"fmt"
	"reflect"

	"github.com/samber/lo"
	"github.com/samber/mo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// getParentStatuses creates a parent status map for the provided route given the
// route parent status slice.
func getParentStatuses[routeT gwtypes.RouteT](
	route routeT, parentStatuses []gwtypes.RouteParentStatus,
) map[string]*gwtypes.RouteParentStatus {
	m := make(map[string]*gwtypes.RouteParentStatus)

	for _, existingParent := range parentStatuses {
		parentRef := getParentRef(existingParent)
		key := routeParentStatusKey(route, parentRef)

		existingParentCopy := existingParent
		m[key] = &existingParentCopy
	}
	return m
}

type namespacedNamer interface {
	GetNamespace() string
	GetName() string
	GetSectionName() mo.Option[string]
}

func routeParentStatusKey[routeT gwtypes.RouteT](
	route routeT, parentRef namespacedNamer,
) string {
	namespace := route.GetNamespace()
	if ns := parentRef.GetNamespace(); ns != "" {
		namespace = ns
	}

	switch any(route).(type) {
	case *gwtypes.HTTPRoute,
		*gwtypes.GRPCRoute:
		return fmt.Sprintf("%s/%s/%s",
			namespace,
			parentRef.GetName(),
			parentRef.GetSectionName().OrEmpty())
	default:
		return fmt.Sprintf("%s/%s", namespace, parentRef.GetName())
	}
}

type parentRef struct {
	Namespace   *string
	Name        string
	SectionName *string
}

func (p parentRef) GetName() string {
	return p.Name
}

func (p parentRef) GetNamespace() string {
	if p.Namespace != nil {
		return *p.Namespace
	}
	return ""
}

func (p parentRef) GetSectionName() mo.Option[string] {
	if p.SectionName != nil {
		return mo.Some(*p.SectionName)
	}
	return mo.None[string]()
}

// getParentRef serves as glue code to generically get parentRef from either
// gwtypes.RouteParentStatus or gwtypes.RouteParentStatus.
func getParentRef(parentStatus gwtypes.RouteParentStatus) parentRef {
	var (
		sectionName *string
		namespace   *string
		ref         = parentStatus.ParentRef
	)
	if ref.SectionName != nil {
		sectionName = new(string(*ref.SectionName))
	}
	if ref.Namespace != nil {
		namespace = new(string(*ref.Namespace))
	}
	return parentRef{
		Namespace:   namespace,
		Name:        string(ref.Name),
		SectionName: sectionName,
	}
}

func getRouteStatusParents[T gwtypes.RouteT](route T) []gwtypes.RouteParentStatus {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		return r.Status.Parents
	case *gwtypes.TCPRoute:
		return r.Status.Parents
	case *gwtypes.UDPRoute:
		return r.Status.Parents
	case *gwtypes.TLSRoute:
		return r.Status.Parents
	case *gwtypes.GRPCRoute:
		return r.Status.Parents
	default:
		return nil
	}
}

func setRouteStatusParents[T gwtypes.RouteT](route T, parents []gwtypes.RouteParentStatus) {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		r.Status.Parents = parents
	case *gwtypes.TCPRoute:
		r.Status.Parents = parents
	case *gwtypes.UDPRoute:
		r.Status.Parents = parents
	case *gwtypes.TLSRoute:
		r.Status.Parents = parents
	case *gwtypes.GRPCRoute:
		r.Status.Parents = parents
	}
}

// parentStatusesForRoute returns route parent statuses for the given route
// and its supported gateways.
// It returns a map of parent statuses and a boolean indicating whether any
// changes were made.
func parentStatusesForRoute[routeT gwtypes.RouteT](
	route routeT,
	routeParentStatuses []gwtypes.RouteParentStatus,
	gateways ...supportedGatewayWithCondition,
) (map[string]*gwtypes.RouteParentStatus, bool) {
	// map the existing parentStatues to avoid duplications
	parentStatuses := getParentStatuses(route, routeParentStatuses)

	// overlay the parent ref statuses for all new gateway references
	statusChangesWereMade := false
	for _, gateway := range gateways {
		// build a new status for the parent Gateway
		gatewayParentStatus := gatewayParentStatusForRoute(route, gateway, withSectionName(gateway.listenerName))

		// if the reference already exists and doesn't require any changes
		// then just leave it alone.
		parentRefKey := routeParentStatusKey(route, gateway)
		if existing, ok := parentStatuses[parentRefKey]; ok {
			// check if the parentRef and controllerName are equal, and whether
			// the new condition is present in existing conditions
			if reflect.DeepEqual(existing.ParentRef, gatewayParentStatus.ParentRef) &&
				existing.ControllerName == gatewayParentStatus.ControllerName &&
				lo.ContainsBy(existing.Conditions, func(condition metav1.Condition) bool {
					return sameCondition(gatewayParentStatus.Conditions[0], condition)
				}) {
				continue
			}
		}

		// otherwise overlay the new status on top the list of parentStatuses
		parentStatuses[parentRefKey] = gatewayParentStatus
		statusChangesWereMade = true
	}
	return parentStatuses, statusChangesWereMade
}

func withSectionName(sectionName string) func(*gwtypes.RouteParentStatus) {
	return func(routeParentStatus *gwtypes.RouteParentStatus) {
		if sectionName != "" {
			routeParentStatus.ParentRef.SectionName = (*gwtypes.SectionName)(&sectionName)
		}
	}
}

func gatewayParentStatusForRoute[routeT gwtypes.RouteT](
	route routeT,
	parentGateway supportedGatewayWithCondition,
	opts ...func(*gwtypes.RouteParentStatus),
) *gwtypes.RouteParentStatus {
	parentGVK := parentGateway.gateway.GroupVersionKind()
	if parentGVK.Kind == "" {
		parentGVK.Kind = gwtypes.V1GatewayTypeMeta.Kind
	}
	if parentGVK.Group == "" {
		parentGVK.Group = gwtypes.V1GatewayTypeMeta.GroupVersionKind().Group
		parentGateway.gateway.SetGroupVersionKind(parentGVK)
	}

	var (
		parentRef = gwtypes.ParentReference{
			Group:     util.StringToTypedPtr[*gwtypes.Group](parentGateway.gateway.GroupVersionKind().Group),
			Kind:      util.StringToTypedPtr[*gwtypes.Kind](parentGateway.gateway.Kind),
			Namespace: (*gwtypes.Namespace)(&parentGateway.gateway.Namespace),
			Name:      gwtypes.ObjectName(parentGateway.gateway.Name),
		}
		routeParentStatus = &gwtypes.RouteParentStatus{
			ParentRef:      parentRef,
			ControllerName: GetControllerName(),
			Conditions: []metav1.Condition{
				{
					Type:               parentGateway.condition.Type,
					Status:             parentGateway.condition.Status,
					ObservedGeneration: route.GetGeneration(),
					LastTransitionTime: metav1.Now(),
					Reason:             parentGateway.condition.Reason,
					Message:            parentGateway.condition.Message,
				},
			},
		}
	)

	for _, opt := range opts {
		opt(routeParentStatus)
	}

	return routeParentStatus
}

func initializeParentStatusesWithProgrammedCondition[routeT gwtypes.RouteT](
	route routeT,
	parentStatuses map[string]*gwtypes.RouteParentStatus,
) bool {
	// do not update the condition if a "Programmed" condition is already present.
	changed := false
	programmedConditionUnknown := newProgrammedConditionUnknown(route)
	for _, ps := range parentStatuses {
		if isForeignParentStatus(ps) {
			continue
		}
		if !parentStatusHasProgrammedCondition(ps) {
			ps.Conditions = append(ps.Conditions, programmedConditionUnknown)
			changed = true
		}
	}
	return changed
}

// isForeignParentStatus returns true if the parent status entry is owned by
// another controller. Such entries must never be modified (see the
// HTTPRoutePreserveForeignStatus conformance test). Entries with an empty
// controller name predate per-controller ownership and are still updated.
func isForeignParentStatus(ps *gwtypes.RouteParentStatus) bool {
	return ps.ControllerName != "" && ps.ControllerName != GetControllerName()
}
