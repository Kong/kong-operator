package gateway

import (
	"slices"

	"github.com/samber/lo"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ProtocolPortsFromListeners returns the ports of the Gateway's listeners for the
// specified protocol (or protocols - useful for HTTP and HTTPS), optionally filtered
// by the parentRef's sectionName and port. These ports become the destination ports
// of the translated Kong stream route so Kong can match incoming traffic by the port
// the parent Gateway listener accepts traffic on. Kong requires at least one of sources,
// destinations or snis on a route whose protocols include the specified protocol,
// so the destinations derived here are what make a protocol-specific KongRoute programmable.
//
// Returns nil when no matching listener for the specified protocol is found.
func ProtocolPortsFromListeners(
	gw *gatewayv1.Gateway, sectionName *gatewayv1.SectionName, port *gatewayv1.PortNumber, protocol ...gatewayv1.ProtocolType,
) []int32 {
	portSet := make(map[int32]struct{})
	for _, l := range gw.Spec.Listeners {
		if sectionName != nil && *sectionName != l.Name {
			continue
		}
		if port != nil && *port != l.Port {
			continue
		}
		if !slices.Contains(protocol, l.Protocol) {
			continue
		}
		portSet[l.Port] = struct{}{}
	}

	if len(portSet) == 0 {
		return nil
	}

	ports := lo.Keys(portSet)
	slices.Sort(ports)

	return ports
}
