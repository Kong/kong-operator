package converter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestWinningL4RoutePortsByParentRefSupportsUDPRoute(t *testing.T) {
	winner := newUDPRouteForL4ConflictTest("winner", time.Unix(1, 0))
	loser := newUDPRouteForL4ConflictTest("loser", time.Unix(2, 0))

	parentRef := winner.Spec.ParentRefs[0]
	listenerKey := l4ListenerKey{
		gatewayNamespace: "default",
		gatewayName:      "test-gateway",
		listenerName:     "udp",
		port:             53,
	}

	portsByParentRef, err := winningL4RoutePortsByParentRef(
		winner,
		[]*gwtypes.UDPRoute{winner, loser},
		[]gwtypes.ParentReference{parentRef},
		func(route *gwtypes.UDPRoute) ([]l4RouteAttachment, error) {
			return []l4RouteAttachment{{
				listenerKey:  listenerKey,
				parentRefKey: l4ParentRefKeyForRoute(route, &route.Spec.ParentRefs[0]),
			}}, nil
		},
	)
	require.NoError(t, err)

	assert.Equal(t, []int32{53}, portsByParentRef[l4ParentRefKeyForRoute(winner, &parentRef)])
}

func TestWinningL4RoutePortsByParentRefReturnsNoPortsForLosingRoute(t *testing.T) {
	winner := newUDPRouteForL4ConflictTest("winner", time.Unix(1, 0))
	loser := newUDPRouteForL4ConflictTest("loser", time.Unix(2, 0))

	parentRef := loser.Spec.ParentRefs[0]
	listenerKey := l4ListenerKey{
		gatewayNamespace: "default",
		gatewayName:      "test-gateway",
		listenerName:     "udp",
		port:             53,
	}

	portsByParentRef, err := winningL4RoutePortsByParentRef(
		loser,
		[]*gwtypes.UDPRoute{winner, loser},
		[]gwtypes.ParentReference{parentRef},
		func(route *gwtypes.UDPRoute) ([]l4RouteAttachment, error) {
			return []l4RouteAttachment{{
				listenerKey:  listenerKey,
				parentRefKey: l4ParentRefKeyForRoute(route, &route.Spec.ParentRefs[0]),
			}}, nil
		},
	)
	require.NoError(t, err)

	assert.Empty(t, portsByParentRef[l4ParentRefKeyForRoute(loser, &parentRef)])
}

func newUDPRouteForL4ConflictTest(name string, creationTimestamp time.Time) *gwtypes.UDPRoute {
	return &gwtypes.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(creationTimestamp),
		},
		Spec: gwtypes.UDPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name:  "test-gateway",
					Kind:  new(gwtypes.Kind("Gateway")),
					Group: new(gwtypes.Group(gwtypes.GroupName)),
				}},
			},
		},
	}
}
