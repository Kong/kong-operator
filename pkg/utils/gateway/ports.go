package gateway

import (
	"slices"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// TCPPortsFromListeners returns the ports of the Gateway's TCP-protocol listeners,
// optionally filtered by the parentRef's sectionName and port. These ports become
// the destination ports of the translated Kong stream route so Kong can match
// incoming TCP connections by the port the parent Gateway listener accepts traffic
// on. Kong requires at least one of sources, destinations or snis on a route whose
// protocols include tcp, so the destinations derived here are what make a
// TCPRoute-generated KongRoute programmable.
//
// Returns nil when no matching TCP listener is found.
func TCPPortsFromListeners(gw *gatewayv1.Gateway, sectionName *gatewayv1.SectionName, port *gatewayv1.PortNumber) []int32 {
	portSet := make(map[int32]struct{})
	for _, l := range gw.Spec.Listeners {
		if sectionName != nil && *sectionName != l.Name {
			continue
		}
		if port != nil && *port != l.Port {
			continue
		}
		if l.Protocol != gatewayv1.TCPProtocolType {
			continue
		}
		portSet[l.Port] = struct{}{}
	}

	if len(portSet) == 0 {
		return nil
	}

	ports := make([]int32, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	slices.Sort(ports)

	return ports
}

// UDPPortsFromListeners returns the ports of the Gateway's UDP-protocol listeners,
// optionally filtered by the parentRef's sectionName and port. These ports become
// the destination ports of the translated Kong stream route so Kong can match
// incoming UDP traffic by the port the parent Gateway listener accepts traffic
// on. Kong requires at least one of sources, destinations or snis on a route whose
// protocols include udp, so the destinations derived here are what make a
// UDPRoute-generated KongRoute programmable.
//
// Returns nil when no matching UDP listener is found.
func UDPPortsFromListeners(gw *gatewayv1.Gateway, sectionName *gatewayv1.SectionName, port *gatewayv1.PortNumber) []int32 {
	portSet := make(map[int32]struct{})
	for _, l := range gw.Spec.Listeners {
		if sectionName != nil && *sectionName != l.Name {
			continue
		}
		if port != nil && *port != l.Port {
			continue
		}
		if l.Protocol != gatewayv1.UDPProtocolType {
			continue
		}
		portSet[l.Port] = struct{}{}
	}

	if len(portSet) == 0 {
		return nil
	}

	ports := make([]int32, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	slices.Sort(ports)

	return ports
}
