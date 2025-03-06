package gateway

import (
	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
)

// -----------------------------------------------------------------------------
// Gateway - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// GatewayServiceType the Gateway service condition type
	GatewayServiceType kcfgconsts.ConditionType = "GatewayService"

	// ControlPlaneReadyType the ControlPlane is deployed and Ready
	ControlPlaneReadyType kcfgconsts.ConditionType = "ControlPlaneReady"

	// DataPlaneReadyType the DataPlane is deployed and Ready
	DataPlaneReadyType kcfgconsts.ConditionType = "DataPlaneReady"
)

// -----------------------------------------------------------------------------
// Gateway - Status Condition Reasons
// -----------------------------------------------------------------------------

const (
	// GatewayReasonServiceError must be used with the GatewayService condition
	// to express that the Gateway Service is not properly configured.
	GatewayReasonServiceError kcfgconsts.ConditionReason = "GatewayServiceError"

	// ListenerReasonTooManyTLSSecrets must be used with the ResolvedRefs condition
	// to express that more than one TLS secret has been set in the listener
	// TLS configuration.
	ListenerReasonTooManyTLSSecrets kcfgconsts.ConditionReason = "TooManyTLSSecrets"
)
