package translator

import (
	"fmt"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/translator/subtranslator"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

// -----------------------------------------------------------------------------
// Translate TCPRoute - IngressRules Translation
// -----------------------------------------------------------------------------

// ingressRulesFromTCPRoutes processes a list of TCPRoute objects and translates
// them into Kong configuration objects.
// Per GEP-2645, when multiple TCPRoutes attached to the same listener, only the
// winner is rendered into the dataplane.
// Winner selection: oldest CreationTimestamp; namespace/name (alphabetical).
func (t *Translator) ingressRulesFromTCPRoutes() ingressRules {
	result := newIngressRules()

	tcpRouteList, err := t.storer.ListTCPRoutes()
	if err != nil {
		t.logger.Error(err, "Failed to list TCPRoutes")
		return result
	}

	var errs []error

	// Validate first; keep only structurally-valid routes for arbitration.
	valid := make([]*gatewayapi.TCPRoute, 0, len(tcpRouteList))
	for _, r := range tcpRouteList {
		if err := validateTCPRoute(r); err != nil {
			errs = append(errs, err)
			t.registerTranslationFailure(err.Error(), r)
			continue
		}
		valid = append(valid, r)
	}

	// Build gateway -> TCP listeners index for the gateways referenced anywhere.
	listenersByGateway := collectL4ListenersByGateway(t.storer, valid, gatewayapi.TCPProtocolType)

	// Group routes by l4ListenerKey.
	attachments := make(map[l4ListenerKey][]*gatewayapi.TCPRoute)
	for _, r := range valid {
		listenerKeys := l4RouteListenerAttachments(r, t.logger, t.storer, listenersByGateway)
		for _, k := range listenerKeys {
			attachments[k] = append(attachments[k], r)
		}
	}

	// Pick a winner per listener, then aggregate the listener ports each
	// winning route owns.
	winningPorts := make(map[*gatewayapi.TCPRoute][]gatewayapi.PortNumber)
	for key, candidates := range attachments {
		winner := pickWinningL4Route(candidates)
		if winner == nil {
			continue
		}
		winningPorts[winner] = append(winningPorts[winner], key.port)
	}

	for _, r := range valid {
		ports, ok := winningPorts[r]
		if !ok {
			continue
		}
		ports = dedupPorts(ports)
		if err := t.translateTCPRouteWithPorts(&result, r, ports); err != nil {
			errs = append(errs, fmt.Errorf("TCPRoute %s/%s can't be routed: %w",
				r.Namespace, r.Name, err))
			continue
		}

		// Only the winner produced Kong entities, so only the winner is reported as
		// successfully translated. A route that loses arbitration on every listener
		// it attached to is left unreported (Programmed stays Unknown) - this way
		// Programmed reflects the route's real dataplane status: it flips to True
		// only once the route actually wins arbitration (e.g. after the current
		// winner is deleted) and its config is genuinely pushed.
		t.registerSuccessfullyTranslatedObject(r)
	}

	if t.featureFlags.ExpressionRoutes {
		applyExpressionToIngressRules(&result)
	}

	for _, err := range errs {
		t.logger.Error(err, "Could not generate route from TCPRoute")
	}

	return result
}

// translateTCPRouteWithPorts emits kong.Route(s) + kong.Service(s) for every
// rule on `route`, with Destinations covering the supplied listener ports.
// Callers must pass only ports for listeners where `route` won arbitration.
func (t *Translator) translateTCPRouteWithPorts(
	result *ingressRules,
	route *gatewayapi.TCPRoute,
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
			t.logger, t.storer, result, route, ruleNumber, "tcp", rule.BackendRefs...)
		if err != nil {
			return err
		}
		service.Routes = append(service.Routes, routes...)

		result.ServiceNameToServices[*service.Name] = service
		result.ServiceNameToParent[*service.Name] = route
	}
	return nil
}

// validateTCPRoute validates TCPRoute, and return a translation error if the spec is invalid.
// Validation for TCPRoutes will happen at a higher layer, but in spite of that we run
// validation at this level as well as a fallback so that if routes are posted which
// are invalid somehow make it past validation (e.g. the webhook is not enabled) we can
// at least try to provide a helpful message about the situation in the manager logs.
func validateTCPRoute(tcproute *gatewayapi.TCPRoute) error {
	if len(tcproute.Spec.Rules) == 0 {
		return subtranslator.ErrRouteValidationNoRules
	}
	for _, rule := range tcproute.Spec.Rules {
		if len(rule.BackendRefs) == 0 {
			return subtranslator.ErrRotueValidationRuleNoBackendRef
		}
	}
	return nil
}
