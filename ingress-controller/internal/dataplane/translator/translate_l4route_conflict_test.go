package translator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	mgrconsts "github.com/kong/kong-operator/v2/ingress-controller/internal/manager/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
)

func mkGateway(ns, name, gatewayClassName string, listeners ...gatewayv1.Listener) *gatewayapi.Gateway {
	return &gatewayapi.Gateway{
		Namespace: ns, Name: name,
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gatewayClassName),
			Listeners:        listeners,
		},
	}
}

func mkGatewayClass(name string, controllerName gatewayv1.GatewayController) *gatewayapi.GatewayClass {
	return &gatewayapi.GatewayClass{
		Name: name,
		Spec: gatewayv1.GatewayClassSpec{ControllerName: controllerName},
	}
}

func TestCollectL4ListenersByGateway(t *testing.T) {
	ownedGWC := mkGatewayClass("owned-gwc", mgrconsts.GetControllerName())
	otherGWC := mkGatewayClass("other-gwc", "example.com/other-controller")

	ownedGW := mkGateway("gw-ns", "owned-gw", "owned-gwc",
		gatewayv1.Listener{Name: "udp", Port: 53, Protocol: gatewayv1.UDPProtocolType},
	)
	unownedGW := mkGateway("gw-ns", "unowned-gw", "other-gwc",
		gatewayv1.Listener{Name: "udp", Port: 53, Protocol: gatewayv1.UDPProtocolType},
	)
	missingGWClassGW := mkGateway("gw-ns", "missing-gwc-gw", "does-not-exist",
		gatewayv1.Listener{Name: "udp", Port: 53, Protocol: gatewayv1.UDPProtocolType},
	)

	storer, err := store.NewFakeStore(store.FakeObjects{
		GatewayClasses: []*gatewayapi.GatewayClass{ownedGWC, otherGWC},
		Gateways:       []*gatewayapi.Gateway{ownedGW, unownedGW, missingGWClassGW},
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		gatewayRef string
		wantCount  int
	}{
		{
			name:       "gateway owned by this controller's GatewayClass is included",
			gatewayRef: "owned-gw",
			wantCount:  1,
		},
		{
			name:       "gateway owned by a different controller's GatewayClass is excluded",
			gatewayRef: "unowned-gw",
			wantCount:  0,
		},
		{
			name:       "gateway referencing a missing GatewayClass is excluded",
			gatewayRef: "missing-gwc-gw",
			wantCount:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route := mkUDPRouteWithParents("app", parentRef("gw-ns", tc.gatewayRef, "", 0))
			got := collectL4ListenersByGateway(storer, []*gatewayapi.UDPRoute{route}, gatewayv1.UDPProtocolType)
			assert.Len(t, got, tc.wantCount)
			if tc.wantCount > 0 {
				assert.Contains(t, got, types.NamespacedName{Namespace: "gw-ns", Name: tc.gatewayRef})
			}
		})
	}
}
