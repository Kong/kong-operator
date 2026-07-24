package translator

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
)

// tL4Route constrains layer-4 Gateway API route types whose listener
// arbitration follows GEP-2645 (single winner per listener; no SNI
// multiplexing). Embedding gatewayapi.RouteT lets this type parameter be
// passed directly to the shared listener-attachment predicates in the
// gatewayapi package.
type tL4Route interface {
	gatewayapi.RouteT
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

// l4Listener pairs a Gateway listener with its owning Gateway's listener
// statuses, so that per-route attachment predicates (AllowedRoutes,
// SupportedKinds, Programmed) can be evaluated later, once a candidate route
// is known.
type l4Listener struct {
	listener gatewayv1.Listener
	gwStatus []gatewayv1.ListenerStatus
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

			// TODO: no GatewayClass-controller-ownership check here (unlike
			// getSupportedGatewayForRoute) - store.Storer doesn't cache
			// GatewayClass at all. A Gateway managed by a different
			// controller in the cluster is still resolved and arbitrated
			// over. Needs a GatewayClass cache/lister added to store.Storer
			// before this can be checked.
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
					listener: l,
					gwStatus: gw.Status.Listeners,
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
// per-Gateway listener index and returns the listener tuples the route
// actually attaches to, applying the same attachment predicates
// getSupportedGatewayForRoute uses (AllowedRoutes Kind/Namespace, listener
// SupportedKinds, listener Programmed) so that the arbitration candidate pool
// matches what the status layer would accept.
//
// AllowedRoutes.Namespaces.From: Selector can't be evaluated here (it needs a
// Namespace cache the translator doesn't have) and is conservatively treated
// as not-attached, logged once per candidate.
func l4RouteListenerAttachments[T tL4Route](
	route T,
	logger logr.Logger,
	listenersByGateway map[types.NamespacedName][]l4Listener,
) []l4ListenerKey {
	var out []l4ListenerKey
	for _, pr := range l4RouteParentRefs(route) {
		gwNN := parentRefGatewayNN(pr, route.GetNamespace())
		listeners, ok := listenersByGateway[gwNN]
		if !ok {
			continue
		}
		for _, l := range listeners {
			if pr.SectionName != nil && string(*pr.SectionName) != string(l.listener.Name) {
				continue
			}
			if pr.Port != nil && *pr.Port != l.listener.Port {
				continue
			}
			if !gatewayapi.ListenerAcceptsRouteKind(l.listener, route) {
				continue
			}
			if ok, handled := gatewayapi.ListenerAllowsNamespace(l.listener, route, gwNN.Namespace, pr.Namespace); handled {
				if !ok {
					continue
				}
			} else {
				logger.V(1).Info(
					"skipping L4 arbitration candidate: listener AllowedRoutes uses a namespace Selector, which the translator can't evaluate",
					"gateway", gwNN, "listener", l.listener.Name,
					"route", route.GetNamespace()+"/"+route.GetName(),
				)
				continue
			}
			if err := gatewayapi.ListenerSupportsRouteInStatus(route, l.listener.Name, l.gwStatus); err != nil {
				continue
			}
			if err := gatewayapi.ListenerProgrammed(l.listener.Name, l.gwStatus); err != nil {
				continue
			}
			out = append(out, l4ListenerKey{
				gateway:      gwNN,
				listenerName: string(l.listener.Name),
				port:         l.listener.Port,
			})
		}
	}
	return out
}
