package controllers

import (
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// Gateway - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// GatewayScheduledType the Gateway has been scheduled
	GatewayScheduledType k8sutils.ConditionType = "GatewayScheduled"

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
	// GatewayServiceErrorReason the Gateway Service is not properly configured
	GatewayServiceErrorReason k8sutils.ConditionReason = "GatewayServiceError"
)
