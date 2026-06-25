package translator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
)

// tL4Route constrains layer-4 Gateway API route types whose listener
// arbitration follows GEP-2645 (single winner per listener; no SNI
// multiplexing). Both pointer types satisfy metav1.Object.
type tL4Route interface {
	metav1.Object
	*gatewayapi.UDPRoute | *gatewayapi.TCPRoute
}

// pickWinningL4Route returns the route that wins GEP-2645 arbitration for a
// listener: the route with the oldest CreationTimestamp; ties broken by
// namespace/name (alphabetical ascending). Returns the zero value when the
// input is empty.
//
// Caller must have already filtered the input to routes whose ParentRef
// matches the same (Gateway, Listener) tuple.
func pickWinningL4Route[T tL4Route](routes []T) T {
	var winner T
	if len(routes) == 0 {
		return winner
	}
	winner = routes[0]
	for _, r := range routes[1:] {
		if l4RouteLess(r, winner) {
			winner = r
		}
	}
	return winner
}

// l4RouteLess returns true if a should sort before b per GEP-2645:
// older creationTimestamp first, then namespace/name ascending.
func l4RouteLess[T tL4Route](a, b T) bool {
	aTS, bTS := a.GetCreationTimestamp(), b.GetCreationTimestamp()
	if !aTS.Equal(&bTS) {
		return aTS.Before(&bTS)
	}
	return a.GetNamespace()+"/"+a.GetName() < b.GetNamespace()+"/"+b.GetName()
}

// l4Listener is a minimal projection of gatewayv1.Listener: only the fields
// needed for layer-4 route arbitration.
type l4Listener struct {
	name string
	port gatewayv1.PortNumber
}

// l4ListenerKey identifies a single Gateway listener: gateway NN + listener
// name + port. Used as a map key to group routes by listener.
type l4ListenerKey struct {
	gateway      types.NamespacedName
	listenerName string
	port         gatewayv1.PortNumber
}

// l4RouteParentRefs returns the ParentRefs of a layer-4 route. Type-switches
// over the concrete route type since UDPRouteSpec and TCPRouteSpec are
// distinct types that share the same CommonRouteSpec shape.
func l4RouteParentRefs[T tL4Route](r T) []gatewayv1.ParentReference {
	switch rr := any(r).(type) {
	case *gatewayapi.UDPRoute:
		return rr.Spec.ParentRefs
	case *gatewayapi.TCPRoute:
		return rr.Spec.ParentRefs
	}
	return nil
}

func parentRefGatewayNN(pr gatewayv1.ParentReference, routeNamespace string) types.NamespacedName {
	ns := routeNamespace
	if pr.Namespace != nil {
		ns = string(*pr.Namespace)
	}
	return types.NamespacedName{Namespace: ns, Name: string(pr.Name)}
}

// collectL4ListenersByGateway resolves every Gateway referenced by any
// ParentRef across the given routes and returns a map keyed by Gateway NN of
// its listeners matching the given protocol. Gateways not found in storer
// are omitted.
func collectL4ListenersByGateway[T tL4Route](
	storer store.Storer,
	routes []T,
	protocol gatewayv1.ProtocolType,
) map[types.NamespacedName][]l4Listener {
	out := make(map[types.NamespacedName][]l4Listener)
	seen := make(map[types.NamespacedName]struct{})
	for _, r := range routes {
		for _, pr := range l4RouteParentRefs(r) {
			gwNN := parentRefGatewayNN(pr, r.GetNamespace())
			if _, ok := seen[gwNN]; ok {
				continue
			}
			seen[gwNN] = struct{}{}

			gw, err := storer.GetGateway(gwNN.Namespace, gwNN.Name)
			if err != nil {
				continue
			}
			var ls []l4Listener
			for _, l := range gw.Spec.Listeners {
				if l.Protocol != protocol {
					continue
				}
				ls = append(ls, l4Listener{
					name: string(l.Name),
					port: l.Port,
				})
			}
			if len(ls) > 0 {
				out[gwNN] = ls
			}
		}
	}
	return out
}

// l4RouteListenerAttachments expands a route's ParentRefs against the supplied
// per-Gateway listener index and returns the listener tuples the route attaches
// to.
func l4RouteListenerAttachments(
	routeNamespace string,
	parentRefs []gatewayv1.ParentReference,
	listenersByGateway map[types.NamespacedName][]l4Listener,
) []l4ListenerKey {
	var out []l4ListenerKey
	for _, pr := range parentRefs {
		gwNN := parentRefGatewayNN(pr, routeNamespace)
		listeners, ok := listenersByGateway[gwNN]
		if !ok {
			continue
		}
		for _, l := range listeners {
			if pr.SectionName != nil && string(*pr.SectionName) != l.name {
				continue
			}
			if pr.Port != nil && *pr.Port != l.port {
				continue
			}
			out = append(out, l4ListenerKey{
				gateway:      gwNN,
				listenerName: l.name,
				port:         l.port,
			})
		}
	}
	return out
}
