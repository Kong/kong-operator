package translator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

func mkUDPRoute(ns, name string, created time.Time) *gatewayapi.UDPRoute {
	return &gatewayapi.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         ns,
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
		},
	}
}

func TestPickWinningUDPRoute(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	older := mkUDPRoute("ns-b", "route-z", t0)
	newer := mkUDPRoute("ns-a", "route-a", t1)
	sameTimeNsA := mkUDPRoute("ns-a", "route-b", t0)
	sameTimeNsB := mkUDPRoute("ns-b", "route-a", t0)
	sameTimeSameNsA := mkUDPRoute("ns-a", "route-a", t0)
	sameTimeSameNsB := mkUDPRoute("ns-a", "route-b", t0)

	tests := []struct {
		name     string
		input    []*gatewayapi.UDPRoute
		wantName string
	}{
		{
			name:     "single route wins",
			input:    []*gatewayapi.UDPRoute{older},
			wantName: "route-z",
		},
		{
			name:     "older creationTimestamp wins regardless of name order",
			input:    []*gatewayapi.UDPRoute{newer, older},
			wantName: "route-z",
		},
		{
			name:     "tied creationTimestamp, namespace alphabetical wins",
			input:    []*gatewayapi.UDPRoute{sameTimeNsB, sameTimeNsA},
			wantName: "route-b",
		},
		{
			name:     "tied creationTimestamp and namespace, name alphabetical wins",
			input:    []*gatewayapi.UDPRoute{sameTimeSameNsB, sameTimeSameNsA},
			wantName: "route-a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pickWinningL4Route(tc.input)
			if assert.NotNil(t, got) {
				assert.Equal(t, tc.wantName, got.Name)
			}
		})
	}
}

func TestPickWinningUDPRouteEmpty(t *testing.T) {
	assert.Nil(t, pickWinningL4Route[*gatewayapi.UDPRoute](nil))
	assert.Nil(t, pickWinningL4Route([]*gatewayapi.UDPRoute{}))
}

func mkUDPRouteWithParents(ns string, parents ...gatewayv1.ParentReference) *gatewayapi.UDPRoute {
	r := &gatewayapi.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "r"},
	}
	r.Spec.ParentRefs = parents
	return r
}

func parentRef(ns, name, section string, port gatewayv1.PortNumber) gatewayv1.ParentReference {
	pr := gatewayv1.ParentReference{Name: gatewayv1.ObjectName(name)}
	if ns != "" {
		n := gatewayv1.Namespace(ns)
		pr.Namespace = &n
	}
	if section != "" {
		s := gatewayv1.SectionName(section)
		pr.SectionName = &s
	}
	if port != 0 {
		p := port
		pr.Port = &p
	}
	return pr
}

func TestUDPRouteListenerAttachments(t *testing.T) {
	gw := types.NamespacedName{Namespace: "gw-ns", Name: "gw"}
	listeners := []l4Listener{
		{name: "l1", port: 53},
		{name: "l2", port: 54},
		{name: "l3", port: 55},
	}

	tests := []struct {
		name        string
		route       *gatewayapi.UDPRoute
		gateways    map[types.NamespacedName][]l4Listener
		wantTargets []l4ListenerKey
	}{
		{
			name: "no SectionName no Port attaches to all UDP listeners on gateway",
			route: mkUDPRouteWithParents("app",
				parentRef("gw-ns", "gw", "", 0),
			),
			gateways: map[types.NamespacedName][]l4Listener{gw: listeners},
			wantTargets: []l4ListenerKey{
				{gateway: gw, listenerName: "l1", port: 53},
				{gateway: gw, listenerName: "l2", port: 54},
				{gateway: gw, listenerName: "l3", port: 55},
			},
		},
		{
			name: "SectionName narrows to one listener",
			route: mkUDPRouteWithParents("app",
				parentRef("gw-ns", "gw", "l2", 0),
			),
			gateways: map[types.NamespacedName][]l4Listener{gw: listeners},
			wantTargets: []l4ListenerKey{
				{gateway: gw, listenerName: "l2", port: 54},
			},
		},
		{
			name: "Port narrows to one listener",
			route: mkUDPRouteWithParents("app",
				parentRef("gw-ns", "gw", "", 55),
			),
			gateways: map[types.NamespacedName][]l4Listener{gw: listeners},
			wantTargets: []l4ListenerKey{
				{gateway: gw, listenerName: "l3", port: 55},
			},
		},
		{
			name: "absent ParentRef namespace defaults to route namespace",
			route: mkUDPRouteWithParents("gw-ns",
				parentRef("", "gw", "l1", 0),
			),
			gateways: map[types.NamespacedName][]l4Listener{gw: listeners},
			wantTargets: []l4ListenerKey{
				{gateway: gw, listenerName: "l1", port: 53},
			},
		},
		{
			name: "parentRef pointing at unknown gateway yields nothing",
			route: mkUDPRouteWithParents("app",
				parentRef("gw-ns", "other-gw", "", 0),
			),
			gateways:    map[types.NamespacedName][]l4Listener{gw: listeners},
			wantTargets: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := l4RouteListenerAttachments(tc.route.Namespace, tc.route.Spec.ParentRefs, tc.gateways)
			assert.ElementsMatch(t, tc.wantTargets, got)
		})
	}
}
