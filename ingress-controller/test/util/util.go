package util

import (
	internalgatewayapi "github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
	internal "github.com/kong/kong-operator/ingress-controller/internal/util"
)

func StringToGatewayAPIKindPtr(kind string) *internalgatewayapi.Kind {
	return internal.StringToGatewayAPIKindPtr(kind)
}
