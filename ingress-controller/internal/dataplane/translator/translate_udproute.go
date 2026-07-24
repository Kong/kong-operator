package translator

import (
	"fmt"
	"slices"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/translator/subtranslator"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

// -----------------------------------------------------------------------------
// Translate UDPRoute - IngressRules Translation
// -----------------------------------------------------------------------------

// ingressRulesFromUDPRoutes processes a list of UDPRoute objects and translates
// them into Kong configuration objects.
// Per GEP-2645, when multiple UDPRoutes attached to the same listener, only the
// winner is rendered into the dataplane.
// Winner selection: oldest CreationTimestamp; namespace/name (alphabetical).
func (t *Translator) ingressRulesFromUDPRoutes() ingressRules {
	result := newIngressRules()

	udpRouteList, err := t.storer.ListUDPRoutes()
	if err != nil {
		t.logger.Error(err, "Failed to list UDPRoutes")
		return result
	}

	// Validate first; keep only structurally-valid routes for arbitration.
	valid := make([]*gatewayapi.UDPRoute, 0, len(udpRouteList))
	for _, r := range udpRouteList {
		if err := validateUDPRoute(r); err != nil {
			t.registerTranslationFailure(err.Error(), r)
			continue
		}
		valid = append(valid, r)
	}

	// Build gateway -> UDP listeners index for the gateways referenced anywhere.
	listenersByGateway := collectL4ListenersByGateway(t.storer, valid, gatewayapi.UDPProtocolType)

	// Group routes by l4ListenerKey.
	attachments := make(map[l4ListenerKey][]*gatewayapi.UDPRoute)
	attachedRoutes := make(map[*gatewayapi.UDPRoute]struct{})
	for _, r := range valid {
		listenerKeys := l4RouteListenerAttachments(r, t.logger, listenersByGateway)
		for _, k := range listenerKeys {
			attachments[k] = append(attachments[k], r)
			attachedRoutes[r] = struct{}{}
		}
	}

	// Pick a winner per listener, then aggregate the listener ports each
	// winning route owns.
	winningPorts := make(map[*gatewayapi.UDPRoute][]gatewayapi.PortNumber)
	for key, candidates := range attachments {
		winner := pickWinningL4Route(candidates)
		if winner == nil {
			continue
		}
		winningPorts[winner] = append(winningPorts[winner], key.port)
	}

	var errs []error
	for _, r := range valid {
		ports, ok := winningPorts[r]
		if !ok {
			continue
		}
		ports = dedupPorts(ports)
		if err := t.translateUDPRouteWithPorts(&result, r, ports); err != nil {
			errs = append(errs, fmt.Errorf("UDPRoute %s/%s can't be routed: %w",
				r.Namespace, r.Name, err))
		}
	}

	// Every UDPRoute that successfully attached to at least one listener (even
	// if it lost arbitration on all of them) is reported as successfully
	// translated — the route is "attached" per spec; arbitration is a
	// translation-layer detail.
	for _, r := range valid {
		if _, ok := attachedRoutes[r]; ok {
			t.registerSuccessfullyTranslatedObject(r)
		}
	}

	if t.featureFlags.ExpressionRoutes {
		applyExpressionToIngressRules(&result)
	}

	for _, err := range errs {
		t.logger.Error(err, "Could not generate route from UDPRoute")
	}

	return result
}

// translateUDPRouteWithPorts emits kong.Route(s) + kong.Service(s) for every
// rule on `route`, with Destinations covering the supplied listener ports.
// Callers must pass only ports for listeners where `route` won arbitration.
func (t *Translator) translateUDPRouteWithPorts(
	result *ingressRules,
	route *gatewayapi.UDPRoute,
	gwPorts []gatewayapi.PortNumber,
) error {
	spec := route.Spec
	if len(spec.Rules) == 0 {
		return subtranslator.ErrRouteValidationNoRules
	}

	for ruleNumber, rule := range spec.Rules {
		routes, err := generateKongRoutesFromRouteRule(route, gwPorts, ruleNumber, rule)
		if err != nil {
			return err
		}
		service, err := generateKongServiceFromBackendRefWithRuleNumber(
			t.logger, t.storer, result, route, ruleNumber, "udp", rule.BackendRefs...)
		if err != nil {
			return err
		}
		service.Routes = append(service.Routes, routes...)

		result.ServiceNameToServices[*service.Name] = service
		result.ServiceNameToParent[*service.Name] = route
	}
	return nil
}

// dedupPorts returns ports with duplicates removed, sorted ascending for
// deterministic output across map-iteration runs.
func dedupPorts(ports []gatewayapi.PortNumber) []gatewayapi.PortNumber {
	if len(ports) == 0 {
		return ports
	}
	seen := make(map[gatewayapi.PortNumber]struct{}, len(ports))
	out := make([]gatewayapi.PortNumber, 0, len(ports))
	for _, p := range ports {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// validateUDPRoute validates UDPRoute, and return a translation error if the spec is invalid.
// Validation for UDPRoutes will happen at a higher layer, but in spite of that we run
// validation at this level as well as a fallback so that if routes are posted which
// are invalid somehow make it past validation (e.g. the webhook is not enabled) we can
// at least try to provide a helpful message about the situation in the manager logs.
func validateUDPRoute(udproute *gatewayapi.UDPRoute) error {
	if len(udproute.Spec.Rules) == 0 {
		return subtranslator.ErrRouteValidationNoRules
	}
	for _, rule := range udproute.Spec.Rules {
		if len(rule.BackendRefs) == 0 {
			return subtranslator.ErrRotueValidationRuleNoBackendRef
		}
	}
	return nil
}
