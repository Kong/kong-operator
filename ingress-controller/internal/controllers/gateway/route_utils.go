package gateway

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/samber/mo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/logging"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	gatewayapipkg "github.com/kong/kong-operator/v2/pkg/gatewayapi"
)

// -----------------------------------------------------------------------------
// Route Utilities
// -----------------------------------------------------------------------------

const (
	ConditionTypeProgrammed                                         = "Programmed"
	ConditionReasonProgrammedUnknown   gwtypes.RouteConditionReason = "Unknown"
	ConditionReasonConfiguredInGateway gwtypes.RouteConditionReason = "ConfiguredInGateway"
	ConditionReasonTranslationError    gwtypes.RouteConditionReason = "TranslationError"
)

var (
	ErrNoMatchingListenerHostname = fmt.Errorf("no matching hostnames in listener")
	ErrNoSupportedGateway         = fmt.Errorf("no supported gateway found for route")
)

// supportedGatewayWithCondition is a struct that wraps a gateway and some further info
// such as the condition Status condition Accepted of the gateway and the listenerName.
type supportedGatewayWithCondition struct {
	gateway               *gwtypes.Gateway
	condition             metav1.Condition
	listenerName          string
	attachedListenerNames []gwtypes.SectionName
}

func (g supportedGatewayWithCondition) GetName() string {
	return g.gateway.GetName()
}

func (g supportedGatewayWithCondition) GetNamespace() string {
	return g.gateway.GetNamespace()
}

func (g supportedGatewayWithCondition) GetSectionName() mo.Option[string] {
	if g.listenerName != "" {
		return mo.Some(g.listenerName)
	}
	return mo.None[string]()
}

// parentRefsForRoute provides a list of the parentRefs given a Gateway APIs route object
// (e.g. HTTPRoute, TCPRoute, e.t.c.) which refer to the Gateway resource(s) which manage it.
//
// parentRefs pointing at a kind this controller cannot resolve (e.g. a ListenerSet, or any
// Gateway API kind introduced after the version this controller is built against) are filtered
// out rather than rejected: such a reference means the route is managed by some other
// controller. Routes left with no supported parentRef are handled by getSupportedGatewayForRoute
// returning ErrNoSupportedGateway, which callers already treat as "not ours". Returning an error
// here instead makes reconciliation of a route this controller does not own fail permanently,
// retrying forever without ever converging.
func parentRefsForRoute[T gwtypes.RouteT](route T) ([]gwtypes.ParentReference, error) {
	var refs []gwtypes.ParentReference
	switch r := (any)(route).(type) {
	case *gwtypes.HTTPRoute:
		refs = r.Spec.ParentRefs
	case *gwtypes.UDPRoute:
		refs = r.Spec.ParentRefs
	case *gwtypes.TCPRoute:
		refs = r.Spec.ParentRefs
	case *gwtypes.TLSRoute:
		refs = r.Spec.ParentRefs
	case *gwtypes.GRPCRoute:
		refs = r.Spec.ParentRefs
	default:
		return nil, fmt.Errorf("can't determine parent Gateway for unsupported route type %s", reflect.TypeOf(route))
	}
	// Filter into a fresh slice rather than in place: refs aliases the route's own
	// Spec.ParentRefs, and the route comes from the shared cache.
	supported := make([]gwtypes.ParentReference, 0, len(refs))
	for _, ref := range refs {
		if isSupportedParentRefKind(ref) {
			supported = append(supported, ref)
		}
	}
	return supported, nil
}

// isSupportedParentRefKind reports whether a parentRef points at a core Gateway API Gateway,
// the only parent kind this controller is able to resolve. Group and Kind are optional in the
// Gateway API and default to gateway.networking.k8s.io and Gateway respectively.
func isSupportedParentRefKind(ref gwtypes.ParentReference) bool {
	group := gatewayv1.GroupName
	if ref.Group != nil && *ref.Group != "" {
		group = string(*ref.Group)
	}
	kind := "Gateway"
	if ref.Kind != nil && *ref.Kind != "" {
		kind = string(*ref.Kind)
	}
	return group == gatewayv1.GroupName && kind == "Gateway"
}

// getSupportedGatewayForRoute will retrieve the Gateway and GatewayClass object for any
// Gateway APIs route object (e.g. HTTPRoute, TCPRoute, e.t.c.) from the provided cached
// client if they match this controller. If there are no gateways present for this route
// OR the present gateways are references to missing objects, this will return a unsupportedGW error.
//
// There is a parameter `specifiedGW` here, which is used to specific the gateway.
func getSupportedGatewayForRoute[T gwtypes.RouteT](
	ctx context.Context, logger logr.Logger, mgrc client.Reader, route T, specifiedGW controllers.OptionalNamespacedName,
) ([]supportedGatewayWithCondition, error) {
	// gather the parentrefs for this route object
	parentRefs, err := parentRefsForRoute(route)
	if err != nil {
		return nil, err
	}

	// search each parentRef to see if this controller is one of the supported ones
	gateways := make([]supportedGatewayWithCondition, 0)
	for _, parentRef := range parentRefs {
		// gather the namespace/name for the gateway
		namespace := route.GetNamespace()
		if parentRef.Namespace != nil {
			// TODO: need namespace restrictions implementation done before
			// merging this, need to filter out objects with a disallowed NS.
			// https://github.com/Kong/kubernetes-ingress-controller/issues/2080
			namespace = string(*parentRef.Namespace)
		}
		name := string(parentRef.Name)

		// If the flag `--gateway-to-reconcile` is set, KIC will only reconcile the specified gateway.
		// https://github.com/Kong/kubernetes-ingress-controller/issues/5322
		if gatewayToReconcile, ok := specifiedGW.Get(); ok {
			if namespace != gatewayToReconcile.Namespace || name != gatewayToReconcile.Name {
				continue
			}
		}

		// pull the Gateway object from the cached client
		gateway := gwtypes.Gateway{}
		if err := mgrc.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		}, &gateway); err != nil {
			if apierrors.IsNotFound(err) {
				// if a configured gateway is not found it's still possible
				// that there's another gateway, so keep searching through the list.
				continue
			}
			return nil, fmt.Errorf("failed to retrieve gateway for route: %w", err)
		}
		gwLogger := logger.WithValues("parentRef.gateway", fmt.Sprintf("%s/%s", gateway.Namespace, gateway.Name))

		// pull the GatewayClass for the Gateway object from the cached client
		gatewayClass := gwtypes.GatewayClass{}
		if err := mgrc.Get(ctx, client.ObjectKey{
			Name: string(gateway.Spec.GatewayClassName),
		}, &gatewayClass); err != nil {
			if apierrors.IsNotFound(err) {
				// if a configured gatewayClass is not found it's still possible
				// that there's another properly configured gateway in the parentRefs,
				// so keep searching through the list.
				continue
			}
			return nil, fmt.Errorf("failed to retrieve gatewayclass for gateway: %w", err)
		}

		// If the GatewayClass does not match this controller then skip it
		if !gatewayapi.GatewayClassControlledBy(&gatewayClass, GetControllerName()) {
			continue
		}

		// Otherwise we're all set and this controller should reconcile this route.

		var (
			// Set to true if there exists a listener which wasn't filtered by:
			// - AlowedRoutes
			// - listener name matching
			// - listener status checks
			// - listener and route type checks
			matched = false
			// Set to true if ParentRef specified a hostname and it matches route's hostnames.
			matchingHostname *metav1.ConditionStatus
			// Set to true if ParentRef specifies a Port and a listener matches that Port.
			portMatched = false

			allowedByAllowedRoutes  = false
			allowedBySupportedKinds = false
			allowedByListenerName   = false

			attachedListenerNames []gwtypes.SectionName
		)

		for _, listener := range gateway.Spec.Listeners {
			listenerLogger := gwLogger.WithValues("listener", string(listener.Name))
			// Check if the route matches listener's AllowedRoutes.
			if ok, err := routeMatchesListenerAllowedRoutes(ctx, mgrc, route, listener, gateway.Namespace, parentRef.Namespace); err != nil {
				return nil, fmt.Errorf("failed matching listener %s to a route %s for gateway %s: %w",
					listener.Name, route.GetName(), gateway.Name, err,
				)
			} else if !ok {
				listenerLogger.V(logging.DebugLevel).Info("Route does not match listener's allowed routes")
				continue
			}
			allowedByAllowedRoutes = true

			// Check the listeners statuses:
			// - Check if a listener status exists with a matching type (via SupportedKinds).
			// - Check if it matches the requested listener by name (if specified).
			if err := existsMatchingListenerInStatus(route, listener, gateway.Status.Listeners); err != nil {
				listenerLogger.V(logging.DebugLevel).Info("Listener does not support this route", "reason", err.Error())
				continue
			} else {
				allowedBySupportedKinds = true
			}

			// Check if listener name matches.
			if parentRef.SectionName != nil {
				if *parentRef.SectionName != "" && *parentRef.SectionName != listener.Name {
					listenerLogger.V(logging.DebugLevel).Info(
						"Listener name does not match parentRef.SectionName",
						"parentRef_sectionName", parentRef.SectionName,
					)
					continue
				}
				allowedByListenerName = true
			}

			// Perform the port matching as described in GEP-957.
			if parentRef.Port != nil {
				if *parentRef.Port != listener.Port {
					// This ParentRef has a port specified and it's different
					// than current listener's port.
					listenerLogger.V(logging.DebugLevel).Info(
						"Listener port does not match parentRef.Port",
						"listener_port", listener.Port, "parentRef_port", parentRef.Port,
					)
					continue
				}
				portMatched = true
			}

			// Check if listener protocol matches
			if !routeTypeMatchesListenerType(route, listener) {
				listenerLogger.V(logging.DebugLevel).Info(
					"Route's type does not match listener's type",
					"route_name", route.GetName(),
				)
				continue
			}

			if routeHostnamesIntersectsWithListenerHostname(route, listener) {
				condTrue := metav1.ConditionTrue
				matchingHostname = &condTrue
			} else {
				condFalse := metav1.ConditionFalse
				matchingHostname = &condFalse
				listenerLogger.V(logging.DebugLevel).Info("Route's hostname does not match listener's hostname")
				continue
			}

			// Every structural check (AllowedRoutes, SupportedKinds, SectionName, Port,
			// protocol, hostname) has passed for this listener - it's the listener this
			// route would attach to, regardless of whether it's Programmed yet. Record it
			// here, before the Programmed check, so a transiently-not-yet-Programmed
			// listener (e.g. while the Gateway controller re-renders listener status) still
			// shows up in attachedListenerNames. isGatewayProgrammedSettledForRoute relies on this
			// to tell "no listener could ever attach this route" (terminal) apart from "the
			// right listener exists but isn't Programmed yet" (transient, should wait).
			attachedListenerNames = append(attachedListenerNames, listener.Name)

			// Check listener readiness last: it must not gate any of the structural checks
			// above, or a listener that structurally matches but is momentarily not
			// Programmed would be indistinguishable, via attachedListenerNames, from a
			// listener that never matched at all.
			if err := listenerProgrammedInStatus(listener.Name, gateway.Status.Listeners); err != nil {
				listenerLogger.V(logging.DebugLevel).Info("Listener is not ready", "reason", err.Error())
				continue
			}

			matched = true
		}

		if matched {
			var listenerName string
			if parentRef.SectionName != nil && *parentRef.SectionName != "" {
				listenerName = string(*parentRef.SectionName)
			}

			condition := metav1.Condition{
				Type:               string(gwtypes.RouteConditionAccepted),
				Status:             metav1.ConditionTrue,
				Reason:             string(gwtypes.RouteReasonAccepted),
				ObservedGeneration: route.GetGeneration(),
			}
			// Kong Gateway cannot retry based on response codes and cannot apply a
			// configurable backoff. Report such configuration on the Route instead of
			// accepting it and silently ignoring those fields.
			if httproute, ok := any(route).(*gwtypes.HTTPRoute); ok {
				if err := gatewayapipkg.ValidateHTTPRouteRetries(httproute.Spec.Rules); err != nil {
					condition = metav1.Condition{
						Type:               string(gwtypes.RouteConditionAccepted),
						Status:             metav1.ConditionFalse,
						Reason:             string(gwtypes.RouteReasonUnsupportedValue),
						Message:            err.Error(),
						ObservedGeneration: route.GetGeneration(),
					}
				}
			}

			gateways = append(gateways, supportedGatewayWithCondition{
				gateway:               &gateway,
				listenerName:          listenerName,
				attachedListenerNames: attachedListenerNames,
				condition:             condition,
			})
		} else {
			// We failed to match a listener with this route

			// This will also catch a case of not matching listener/section name.
			//
			// Cases are ordered to match the sequence in which the loop above checks a
			// listener, not by RouteReason alphabetically or arbitrarily: each flag can
			// only become true for a listener that has already passed every check before
			// it, so "flag N is false across all listeners" means no listener got past
			// check N. Checking flags out of loop-order would let a listener that failed
			// an earlier check (e.g. AllowedRoutes/kind) masquerade as having failed a
			// later one (e.g. SectionName) purely because its later flag was never
			// reached, reporting the wrong reason. matchingHostname is the exception: it's
			// only set on a listener that already passed every other check (it's the last
			// check in the loop), so when non-nil it unambiguously means that listener
			// failed on hostname alone and can safely stay checked first.
			reason := gwtypes.RouteReasonNoMatchingParent
			switch {
			case matchingHostname != nil && *matchingHostname == metav1.ConditionFalse:
				// If there is no matchingHostname, the gateway Status Condition Accepted
				// must be set to False with reason NoMatchingListenerHostname
				reason = gwtypes.RouteReasonNoMatchingListenerHostname
			case !allowedByAllowedRoutes || !allowedBySupportedKinds:
				reason = gwtypes.RouteReasonNotAllowedByListeners
			case parentRef.SectionName != nil && !allowedByListenerName:
				// If ParentRef specified listener names but none of the listeners matches the name,
				// the gateway Status Condition Accepted must be set to False with reason RouteReasonNoMatchingParent.
				reason = gwtypes.RouteReasonNoMatchingParent
			case parentRef.Port != nil && !portMatched:
				// If ParentRef specified a Port but none of the listeners matched, the gateway Status
				// Condition Accepted must be set to False with reason NoMatchingListenerPort
				reason = gwtypes.RouteReasonNoMatchingParent
			case len(attachedListenerNames) > 0:
				// equals to listenerReady=false
				reason = gwtypes.RouteReasonNotAllowedByListeners
			}

			var listenerName string
			if parentRef.SectionName != nil && *parentRef.SectionName != "" {
				listenerName = string(*parentRef.SectionName)
			}

			gateways = append(gateways, supportedGatewayWithCondition{
				gateway:               &gateway,
				listenerName:          listenerName,
				attachedListenerNames: attachedListenerNames,
				condition: metav1.Condition{
					Type:               string(gwtypes.RouteConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             string(reason),
					ObservedGeneration: route.GetGeneration(),
				},
			})
		}
	}

	if len(gateways) == 0 {
		return nil, ErrNoSupportedGateway
	}

	return gateways, nil
}

func routeHostnamesIntersectsWithListenerHostname[T gwtypes.RouteT](route T, listener gwtypes.Listener) bool {
	switch r := (any)(route).(type) {
	case *gwtypes.HTTPRoute:
		return listenerHostnameIntersectWithRouteHostnames(listener, r.Spec.Hostnames)
	case *gwtypes.TCPRoute:
		return true
	case *gwtypes.UDPRoute:
		return true
	case *gwtypes.TLSRoute:
		return listenerHostnameIntersectWithRouteHostnames(listener, r.Spec.Hostnames)
	case *gwtypes.GRPCRoute:
		return listenerHostnameIntersectWithRouteHostnames(listener, r.Spec.Hostnames)
	default:
		return false
	}
}

func routeTypeMatchesListenerType[T gwtypes.RouteT](route T, listener gwtypes.Listener) bool {
	switch (any)(route).(type) {
	case *gwtypes.HTTPRoute:
		// HTTPRoutes support Terminate only
		// Note: this is a guess we are doing as the upstream documentation is unclear at the moment.
		// see https://github.com/kubernetes-sigs/gateway-api/issues/1474
		if listener.Protocol != gwtypes.HTTPProtocolType && listener.Protocol != gwtypes.HTTPSProtocolType {
			return false
		}
		if listener.TLS != nil && *listener.TLS.Mode != gwtypes.TLSModeTerminate {
			return false
		}
	case *gwtypes.TCPRoute:
		if listener.Protocol != gwtypes.TCPProtocolType {
			return false
		}
		// TCPRoutes support Terminate only
		// Note: this is a guess we are doing as the upstream documentation is unclear at the moment.
		// see https://github.com/kubernetes-sigs/gateway-api/issues/1474
		if listener.TLS != nil && *listener.TLS.Mode != gwtypes.TLSModeTerminate {
			return false
		}
	case *gwtypes.UDPRoute:
		if listener.Protocol != gwtypes.UDPProtocolType {
			return false
		}
		// TLS should not be set in UDP listeners
		if listener.TLS != nil {
			return false
		}
	case *gwtypes.TLSRoute:
		if listener.Protocol != gwtypes.TLSProtocolType {
			return false
		}
	case *gwtypes.GRPCRoute:
		if listener.Protocol != gwtypes.HTTPSProtocolType && listener.Protocol != gwtypes.HTTPProtocolType {
			return false
		}
	default:
		return false
	}
	return true
}

// routeMatchesListenerAllowedRoutes checks if the provided route matches the
// criteria defined in listener's AllowedRoutes field.
func routeMatchesListenerAllowedRoutes[T gwtypes.RouteT](
	ctx context.Context,
	mgrc client.Reader,
	route T,
	listener gwtypes.Listener,
	gatewayNamespace string,
	parentRefNamespace *gwtypes.Namespace,
) (bool, error) {
	if !gatewayapi.ListenerAcceptsRouteKind(listener, route) {
		return false, nil
	}

	getNamespace := func(name string) (*corev1.Namespace, error) {
		namespace := &corev1.Namespace{}
		if err := mgrc.Get(ctx, client.ObjectKey{Name: name}, namespace); err != nil {
			return nil, err
		}
		return namespace, nil
	}

	return gatewayapi.ListenerAllowsNamespace(listener, route, gatewayNamespace, parentRefNamespace, getNamespace)
}

// existsMatchingListenerInStatus checks if:
// - If a listener status exists with a matching type (via SupportedKinds).
// - If it matches the requested listener by name (if specified).
func existsMatchingListenerInStatus[T gwtypes.RouteT](route T, listener gwtypes.Listener, lss []gwtypes.ListenerStatus) error {
	return gatewayapi.ListenerSupportsRouteInStatus(route, listener.Name, lss)
}

func listenerProgrammedInStatus(listenerName gwtypes.SectionName, lss []gwtypes.ListenerStatus) error {
	return gatewayapi.ListenerProgrammed(listenerName, lss)
}

func listenerHostnameIntersectWithRouteHostnames[H gwtypes.HostnameT](listener gwtypes.Listener, hostnames []H) bool {
	if len(hostnames) == 0 {
		return true
	}

	// if the listener has no hostname, all hostnames automatically intersect
	if listener.Hostname == nil || *listener.Hostname == "" {
		return true
	}

	// iterate over all the hostnames and check that at least one intersect with the listener hostname
	for _, hostname := range hostnames {
		if util.HostnamesIntersect(*listener.Hostname, hostname) {
			return true
		}
	}

	return false
}

// isListenerHostnameEffective returns true if the listener can specify an effective
// hostname to match hostnames in requests.
// It basically checks if the listener is using any these protocols: HTTP, HTTPS, or TLS.
func isListenerHostnameEffective(listener gwtypes.Listener) bool {
	return listener.Protocol == gwtypes.HTTPProtocolType ||
		listener.Protocol == gwtypes.HTTPSProtocolType ||
		listener.Protocol == gwtypes.TLSProtocolType
}

// routeWithHostnames constrains filterHostnames to the route kinds that carry
// a `Spec.Hostnames []gwtypes.Hostname` field — currently HTTPRoute and TLSRoute.
type routeWithHostnames interface {
	*gwtypes.HTTPRoute | *gwtypes.TLSRoute
}

// filterHostnames accepts a route and returns the same object with only the subset
// of hostnames that intersects with the listeners' hostnames.
// It returns an error if the intersection of hostname match in the route and listeners is empty.
func filterHostnames[T routeWithHostnames](gateways []supportedGatewayWithCondition, route T) (T, error) {
	var (
		specHostnames []gwtypes.Hostname
		setHostnames  func([]gwtypes.Hostname)
	)
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		specHostnames = r.Spec.Hostnames
		setHostnames = func(h []gwtypes.Hostname) { r.Spec.Hostnames = h }
	case *gwtypes.TLSRoute:
		specHostnames = r.Spec.Hostnames
		setHostnames = func(h []gwtypes.Hostname) { r.Spec.Hostnames = h }
	}

	filteredHostnames := make([]gwtypes.Hostname, 0)
	// if no hostnames are specified in the route spec, we use the UNION of all hostnames in supported gateways.
	// if any of supported listener has not specified hostname, the route hostnames remain empty
	// to match **ANY** hostname.
	if len(specHostnames) == 0 {
		var matchAnyHost bool
		filteredHostnames, matchAnyHost = getUnionOfGatewayHostnames(gateways)
		if matchAnyHost {
			return route, nil
		}
	} else {
		for _, hostname := range specHostnames {
			if hostnameMatching := getMinimumHostnameIntersection(gateways, hostname); hostnameMatching != "" {
				filteredHostnames = append(filteredHostnames, hostnameMatching)
			}
		}
		if len(filteredHostnames) == 0 {
			return route, ErrNoMatchingListenerHostname
		}
	}

	setHostnames(filteredHostnames)
	return route, nil
}

// getUnionOfGatewayHostnames returns UNION of hostnames specified in supported gateways.
// the second return value is true if any hostname could be matched in at least one listener
// in supported gateways and listeners, so the `HTTPRoute` could match any hostname.
func getUnionOfGatewayHostnames(gateways []supportedGatewayWithCondition) ([]gwtypes.Hostname, bool) {
	hostnames := make([]gwtypes.Hostname, 0)
	for _, gateway := range gateways {
		if gateway.listenerName != "" {
			if listener := extractListenerSpecFromGateway(
				gateway.gateway,
				gwtypes.SectionName(gateway.listenerName),
			); listener != nil {
				// return true if the listener has not specified hostname to match any hostname.
				if listener.Hostname == nil {
					return nil, true
				}
				hostnames = append(hostnames, *listener.Hostname)
			}
		} else {
			for _, listener := range gateway.gateway.Spec.Listeners {
				// here we consider ALL listeners that are able to configure a hostname if no listener attached.
				// may be changed if there is a conclusion on the upstream discussion about it:
				// https://github.com/kubernetes-sigs/gateway-api/discussions/1563
				if isListenerHostnameEffective(listener) {
					if listener.Hostname == nil {
						return nil, true
					}
					hostnames = append(hostnames, *listener.Hostname)
				}
			}
		}
	}
	return hostnames, false
}

// getMinimumHostnameIntersection returns the minimum intersecting hostname, in the sense that:
//
// - if the listener hostname is empty, return the httpRoute hostname
// - if the listener hostname acts as a wildcard for the httpRoute hostname, return the httpRoute hostname
// - if the httpRoute hostname acts as a wildcard for the listener hostname, return the listener hostname
// - if the httpRoute hostname is the same of the listener hostname, return it
// - if none of the above is true, return an empty string.
func getMinimumHostnameIntersection(gateways []supportedGatewayWithCondition, hostname gwtypes.Hostname) gwtypes.Hostname {
	for _, gateway := range gateways {
		for _, listener := range gateway.gateway.Spec.Listeners {
			// if the listenerName is specified and matches the name of the gateway listener proceed
			if (gwtypes.SectionName)(gateway.listenerName) == "" ||
				(gwtypes.SectionName)(gateway.listenerName) == (listener.Name) {
				if listener.Hostname == nil || *listener.Hostname == "" {
					return hostname
				}
				if util.HostnamesMatch(string(*listener.Hostname), string(hostname)) {
					return hostname
				}
				if util.HostnamesMatch(string(hostname), string(*listener.Hostname)) {
					return (*listener.Hostname)
				}
			}
		}
	}
	return ""
}

func isRouteAccepted(gateways []supportedGatewayWithCondition) bool {
	for _, gateway := range gateways {
		if gateway.condition.Type == string(gwtypes.RouteConditionAccepted) && gateway.condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// isGatewayProgrammedSettledForRoute checks both programmed status of Gateway and its listeners,
// settled means ready or permanently failed.
// It returns false if it's intermittently failed, so that we should requeue and waiting.
// And returns true if it's ready or permanently failed, then we should stop waiting and let
// normal Accepted logic decide the terminal status.
func isGatewayProgrammedSettledForRoute(gateway supportedGatewayWithCondition) bool {
	if !isGatewayProgrammed(gateway.gateway) {
		return false
	}

	// If the Gateway spec has no listener that could attach this route, let normal
	// route acceptance handling set the terminal status. This readiness check is
	// only intended to wait for listener status to catch up when an attached spec
	// listener exists but its status is not programmed yet.
	if len(gateway.attachedListenerNames) == 0 {
		return true
	}

	generation := gateway.gateway.Generation
	transientNotProgrammed := false
	for _, listenerName := range gateway.attachedListenerNames {
		// Any one attached listener being Programmed is enough to proceed - mirrors `isRouteAccepted`.
		if err := listenerProgrammedInStatus(listenerName, gateway.gateway.Status.Listeners); err == nil {
			return true
		}
		// A listener whose ResolvedRefs condition is explicitly False will never
		// become Programmed until that reference is fixed. The same holds for other
		// listener validation failures with dedicated reasons.
		if gatewayapi.ListenerResolvedRefsFalse(listenerName, generation, gateway.gateway.Status.Listeners) ||
			gatewayapi.ListenerProgrammedFalseForSettledReason(listenerName, generation, gateway.gateway.Status.Listeners) {
			continue
		}
		// Transient not Programmed - keep looking for a Programmed sibling first.
		transientNotProgrammed = true
	}
	return !transientNotProgrammed
}

// isHTTPReferenceGranted checks that the backendRef referenced by the HTTPRoute is granted by a ReferenceGrant.
func isHTTPReferenceGranted(grantSpec gwtypes.ReferenceGrantSpec, backendRef gwtypes.HTTPBackendRef, fromNamespace string) bool {
	var backendRefGroup gwtypes.Group
	var backendRefKind gwtypes.Kind

	if backendRef.Group != nil {
		backendRefGroup = *backendRef.Group
	}
	if backendRef.Kind != nil {
		backendRefKind = *backendRef.Kind
	}
	for _, from := range grantSpec.From {
		if from.Group != gatewayv1.GroupName || from.Kind != "HTTPRoute" || fromNamespace != string(from.Namespace) {
			continue
		}

		for _, to := range grantSpec.To {
			if backendRefGroup == to.Group &&
				backendRefKind == to.Kind &&
				(to.Name == nil || *to.Name == backendRef.Name) {
				return true
			}
		}
	}
	return false
}

// isTLSReferenceGranted checks that the backendRef referenced by the TLSRoute is granted by a ReferenceGrant.
func isTLSReferenceGranted(grantSpec gwtypes.ReferenceGrantSpec, backendRef gwtypes.BackendRef, fromNamespace string) bool {
	var backendRefGroup gwtypes.Group
	var backendRefKind gwtypes.Kind

	if backendRef.Group != nil {
		backendRefGroup = *backendRef.Group
	}
	if backendRef.Kind != nil {
		backendRefKind = *backendRef.Kind
	}
	for _, from := range grantSpec.From {
		if from.Group != gatewayv1.GroupName || from.Kind != "TLSRoute" || fromNamespace != string(from.Namespace) {
			continue
		}

		for _, to := range grantSpec.To {
			if backendRefGroup == to.Group &&
				backendRefKind == to.Kind &&
				(to.Name == nil || *to.Name == backendRef.Name) {
				return true
			}
		}
	}
	return false
}

// isUDPReferenceGranted checks that the backendRef referenced by the UDPRoute is granted by a ReferenceGrant.
func isUDPReferenceGranted(grantSpec gwtypes.ReferenceGrantSpec, backendRef gwtypes.BackendRef, fromNamespace string) bool {
	var backendRefGroup gwtypes.Group
	var backendRefKind gwtypes.Kind

	if backendRef.Group != nil {
		backendRefGroup = *backendRef.Group
	}
	if backendRef.Kind != nil {
		backendRefKind = *backendRef.Kind
	}
	for _, from := range grantSpec.From {
		if from.Group != gatewayv1.GroupName || from.Kind != "UDPRoute" || fromNamespace != string(from.Namespace) {
			continue
		}

		for _, to := range grantSpec.To {
			if backendRefGroup == to.Group &&
				backendRefKind == to.Kind &&
				(to.Name == nil || *to.Name == backendRef.Name) {
				return true
			}
		}
	}
	return false
}

// isTCPReferenceGranted checks that the backendRef referenced by the TCPRoute is granted by a ReferenceGrant.
func isTCPReferenceGranted(grantSpec gwtypes.ReferenceGrantSpec, backendRef gwtypes.BackendRef, fromNamespace string) bool {
	var backendRefGroup gwtypes.Group
	var backendRefKind gwtypes.Kind

	if backendRef.Group != nil {
		backendRefGroup = *backendRef.Group
	}
	if backendRef.Kind != nil {
		backendRefKind = *backendRef.Kind
	}
	for _, from := range grantSpec.From {
		if from.Group != gatewayv1.GroupName || from.Kind != "TCPRoute" || fromNamespace != string(from.Namespace) {
			continue
		}

		for _, to := range grantSpec.To {
			if backendRefGroup == to.Group &&
				backendRefKind == to.Kind &&
				(to.Name == nil || *to.Name == backendRef.Name) {
				return true
			}
		}
	}
	return false
}

// setRouteParentInStatusForParent checks if the provided route Status, contains
// status for the provided parent and if it does it sets it to the provided
// RouteStatusParent. If it does not then it appends the provided RouteStatusParent
// to provided route's Status.Parents field.
//
// This might come in useful when the caller wants to set only one parent's
// status.
func setRouteParentInStatusForParent[
	routeT gwtypes.RouteT,
	parentT namespacedNamer,
](
	route routeT,
	routeStatusParent gwtypes.RouteParentStatus,
	parent parentT,
) {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		r.Status.Parents = ensureRoutesParents(r.Status.Parents, routeStatusParent, parent)
	case *gwtypes.TCPRoute:
		r.Status.Parents = ensureRoutesParents(r.Status.Parents, routeStatusParent, parent)
	case *gwtypes.UDPRoute:
		r.Status.Parents = ensureRoutesParents(r.Status.Parents, routeStatusParent, parent)
	case *gwtypes.TLSRoute:
		r.Status.Parents = ensureRoutesParents(r.Status.Parents, routeStatusParent, parent)
	case *gwtypes.GRPCRoute:
		r.Status.Parents = ensureRoutesParents(r.Status.Parents, routeStatusParent, parent)
	}
}

// ensureRoutesParents ensures that the provided RouteStatusParents are updated.
// This function checks if the provided []RouteStatusParents contains a parentRef
// status for the provided status.
// If it doesn't then it adds it to the provided []RouteStatusParents and returns it.
// If it does then it overwrites the provious status for that parent and returns
// the updates []RouteStatusParents.
func ensureRoutesParents[
	parentT namespacedNamer,
](
	routeStatusParents []gwtypes.RouteParentStatus,
	routeStatusParent gwtypes.RouteParentStatus,
	parent parentT,
) []gwtypes.RouteParentStatus {
	for i, p := range routeStatusParents {
		if ensureParentsStatusUpdated(p.ParentRef, parent, routeStatusParents, i, routeStatusParent) {
			return routeStatusParents
		}
	}
	routeStatusParents = append(routeStatusParents, routeStatusParent)
	return routeStatusParents
}

func ensureParentsStatusUpdated[
	parentT namespacedNamer,
](
	parentRef gwtypes.ParentReference,
	parent parentT,
	routeStatusParents []gwtypes.RouteParentStatus,
	i int,
	routeStatusParent gwtypes.RouteParentStatus,
) bool {
	if !isParentRefEqualToParent(parentRef, parent) {
		return false
	}

	routeStatusParents[i] = routeStatusParent
	return true
}

func isParentRefEqualToParent[
	parentT namespacedNamer,
](
	parentRef gwtypes.ParentReference,
	parent parentT,
) bool {
	if *parentRef.Group != gatewayv1.GroupName {
		return false
	}
	if *parentRef.Kind != "Gateway" {
		return false
	}
	if string(parentRef.Name) != parent.GetName() {
		return false
	}
	if parentRef.Namespace != nil && string(*parentRef.Namespace) != parent.GetNamespace() {
		return false
	}
	if parentRef.SectionName != nil && string(*parentRef.SectionName) != parent.GetSectionName().OrEmpty() {
		return false
	}

	return true
}

// isRouteAcceptedByListener checks the given route is accepted by the
// gateway's listener specified by a proper parentReference.
func isRouteAcceptedByListener[T gwtypes.RouteT](ctx context.Context,
	mgrc client.Client,
	route T,
	gateway gwtypes.Gateway,
	listenerIndex int,
	parentRef gwtypes.ParentReference,
) (bool, error) {
	// Check if the route matches listener's AllowedRoutes.
	listener := gateway.Spec.Listeners[listenerIndex]
	if ok, err := routeMatchesListenerAllowedRoutes(ctx, mgrc, route, listener, gateway.Namespace, parentRef.Namespace); err != nil {
		return false, fmt.Errorf("failed matching listener %s to a route %s for gateway %s: %w",
			listener.Name, route.GetName(), gateway.Name, err,
		)
	} else if !ok {
		return false, nil
	}

	// Check the listeners statuses:
	// - Check if a listener status exists with a matching type (via SupportedKinds).
	// - Check if it matches the requested listener by name (if specified).
	if err := existsMatchingListenerInStatus(route, listener, gateway.Status.Listeners); err != nil {
		// return no error here, as we don't care of the reason why this check failed.
		return false, nil //nolint:nilerr
	}

	// Check if listener name matches.
	if parentRef.SectionName != nil && *parentRef.SectionName != "" && *parentRef.SectionName != listener.Name {
		return false, nil
	}

	// Perform the port matching as described in GEP-957.
	if parentRef.Port != nil && *parentRef.Port != listener.Port {
		// This ParentRef has a port specified and it's different
		// than current listener's port.
		return false, nil
	}

	if !routeTypeMatchesListenerType(route, listener) {
		return false, nil
	}

	if !routeHostnamesIntersectsWithListenerHostname(route, listener) {
		return false, nil
	}

	return true, nil
}

// ensureGatewayReferenceStatusRemoved uses the ControllerName provided by the Gateway
// implementation to prune status references to Gateways supported by this controller
// in the provided route.
// When gatewayNN is set, only status entries whose parentRef matches the specified
// Gateway are removed. This prevents one KIC instance from removing statuses set
// by another instance that manages a different Gateway.
func ensureGatewayReferenceStatusRemoved[routeT gwtypes.RouteT](
	ctx context.Context, cl client.Client, log logr.Logger, route routeT, gatewayNN controllers.OptionalNamespacedName,
) (bool, error) {
	debug(log, route, "Unsupported route found, processing to verify whether it was ever supported")
	kind := route.GetObjectKind().GroupVersionKind().Kind
	parents := getRouteStatusParents(route)

	// Drop all status references to supported Gateway objects.
	newStatuses := make([]gwtypes.RouteParentStatus, 0)
	for _, status := range parents {
		if status.ControllerName != GetControllerName() {
			newStatuses = append(newStatuses, status)
			continue
		}
		// When gatewayNN is set, only remove statuses for the specific gateway
		// this controller instance manages.
		if gNN, ok := gatewayNN.Get(); ok {
			if !parentRefMatchesGatewayNN(status.ParentRef, route.GetNamespace(), gNN) {
				newStatuses = append(newStatuses, status)
				continue
			}
		}
		parentRefNN := string(status.ParentRef.Name)
		if status.ParentRef.Namespace != nil {
			parentRefNN = fmt.Sprintf("%s/%s", *status.ParentRef.Namespace, parentRefNN)
		}
		debug(log, route, "Removing parentRef from route status", "parentRef", parentRefNN, "kind", kind)
	}

	// If the new list of statuses is the same length as the old
	// nothing has changed and we're all done.
	if len(newStatuses) == len(parents) {
		return false, nil
	}

	// If the route doesn't have a supported Gateway+GatewayClass associated with
	// it it's possible it became orphaned after becoming queued. In either case
	// ensure that it's removed from the proxy cache to avoid orphaned data-plane
	// configurations.
	setRouteStatusParents(route, newStatuses)
	if err := cl.Status().Update(ctx, route); err != nil {
		return false, fmt.Errorf("failed to remove Gateway parentRef from %s status: %w", kind, err)
	}

	debug(log, route, "Unsupported route was previously supported, status was updated")
	// The status needed to be updated and it was updated successfully.
	return true, nil
}

func getRouteParentRefs[T gwtypes.RouteT](route T) []gwtypes.ParentReference {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		return r.Spec.ParentRefs
	case *gwtypes.TCPRoute:
		return r.Spec.ParentRefs
	case *gwtypes.UDPRoute:
		return r.Spec.ParentRefs
	case *gwtypes.TLSRoute:
		return r.Spec.ParentRefs
	case *gwtypes.GRPCRoute:
		return r.Spec.ParentRefs
	default:
		return nil
	}
}

// parentRefMatchesGatewayNN returns true if the given parentRef references the
// specified gateway. routeNamespace is used as the default namespace when
// parentRef.Namespace is nil.
func parentRefMatchesGatewayNN(parentRef gwtypes.ParentReference, routeNamespace string, gatewayNN k8stypes.NamespacedName) bool {
	ns := routeNamespace
	if parentRef.Namespace != nil {
		ns = string(*parentRef.Namespace)
	}
	return ns == gatewayNN.Namespace && string(parentRef.Name) == gatewayNN.Name
}
