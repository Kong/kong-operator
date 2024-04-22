package gateway

import (
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// Gateway - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// GatewayServiceType the Gateway service condition type
	GatewayServiceType k8sutils.ConditionType = "GatewayService"

	// ControlPlaneReadyType the ControlPlane is deployed and Ready
	ControlPlaneReadyType k8sutils.ConditionType = "ControlPlaneReady"

	// DataPlaneReadyType the DataPlane is deployed and Ready
	DataPlaneReadyType k8sutils.ConditionType = "DataPlaneReady"
)

// -----------------------------------------------------------------------------
// Gateway - Status Condition Reasons
// -----------------------------------------------------------------------------

const (
	// GatewayReasonServiceError must be used with the GatewayService condition
	// to express that the Gateway Service is not properly configured.
	GatewayReasonServiceError k8sutils.ConditionReason = "GatewayServiceError"

	// ListenerReasonTooManyTLSSecrets must be used with the ResolvedRefs condition
	// to express that more than one TLS secret has been set in the listener
	// TLS configuration.
	ListenerReasonTooManyTLSSecrets k8sutils.ConditionReason = "TooManyTLSSecrets"

	// ListenereReasonInvalidTLSMode must be used with the Accepted condition
	// to express that the listener has an invalid TLS mode.
	// HTTPS can only be configured with mode Terminate, while TLS can only be
	// be configured with mode Passthrough.
	ListenereReasonInvalidTLSMode k8sutils.ConditionReason = "InvalidTLSMode"
)
