package types

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type (
	Gateway              = gatewayv1.Gateway
	GatewayAddress       = gatewayv1.GatewayAddress
	GatewayStatusAddress = gatewayv1.GatewayStatusAddress
)
