package gateway

import (
	internal "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/gateway"
	internalgatewayapi "github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

type GatewayReconciler = internal.GatewayReconciler
type HTTPRouteReconciler = internal.HTTPRouteReconciler

func GetControllerName() internalgatewayapi.GatewayController {
	return internal.GetControllerName()
}
