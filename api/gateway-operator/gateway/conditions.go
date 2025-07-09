package gateway

import "github.com/kong/kubernetes-configuration/v2/api/common/consts"

// -----------------------------------------------------------------------------
// Gateway - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// GatewayServiceType the Gateway service condition type
	GatewayServiceType consts.ConditionType = "GatewayService"

	// ControlPlaneReadyType the ControlPlane is deployed and Ready
	ControlPlaneReadyType consts.ConditionType = "ControlPlaneReady"

	// DataPlaneReadyType the DataPlane is deployed and Ready
	DataPlaneReadyType consts.ConditionType = "DataPlaneReady"
)

// -----------------------------------------------------------------------------
// Gateway - Status Condition Reasons
// -----------------------------------------------------------------------------

const (
	// GatewayReasonServiceError must be used with the GatewayService condition
	// to express that the Gateway Service is not properly configured.
	GatewayReasonServiceError consts.ConditionReason = "GatewayServiceError"

	// ListenerReasonTooManyTLSSecrets must be used with the ResolvedRefs condition
	// to express that more than one TLS secret has been set in the listener
	// TLS configuration.
	ListenerReasonTooManyTLSSecrets consts.ConditionReason = "TooManyTLSSecrets"
)
