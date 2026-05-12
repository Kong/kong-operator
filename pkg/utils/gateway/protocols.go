package gateway

import (
	"slices"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ProtocolsFromListeners derives Kong route protocol strings from a Gateway's listeners,
// optionally filtered by sectionName. It maps:
//   - HTTP  → "http"
//   - HTTPS → "https"
//
// Returns nil when no matching listeners are found.
func ProtocolsFromListeners(gw *gatewayv1.Gateway, sectionName *gatewayv1.SectionName) []string {
	protoSet := make(map[string]struct{})
	for _, l := range gw.Spec.Listeners {
		if sectionName != nil && *sectionName != l.Name {
			continue
		}
		switch l.Protocol {
		case gatewayv1.HTTPProtocolType:
			protoSet["http"] = struct{}{}
		case gatewayv1.HTTPSProtocolType:
			protoSet["https"] = struct{}{}
		default:
			// Unsupported protocol, skip.
			continue
		}

	}

	if len(protoSet) == 0 {
		return nil
	}

	protocols := make([]string, 0, len(protoSet))
	for p := range protoSet {
		protocols = append(protocols, p)
	}
	slices.Sort(protocols)

	return protocols
}
