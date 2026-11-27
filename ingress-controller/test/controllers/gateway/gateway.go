package gateway

import (
	internal "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/gateway"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

type GatewayReconciler = internal.GatewayReconciler
type HTTPRouteReconciler = internal.HTTPRouteReconciler
type TLSRouteReconciler = internal.TLSRouteReconciler
type UDPRouteReconciler = internal.UDPRouteReconciler

func GetControllerName() gwtypes.GatewayController {
	return internal.GetControllerName()
}
