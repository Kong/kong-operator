package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

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

// gatewayDecorator Decorator object to add additional functionality to the base k8s Gateway
type gatewayDecorator struct {
	*gatewayv1beta1.Gateway
}

func (g *gatewayDecorator) GetConditions() []metav1.Condition {
	return g.Status.Conditions
}

func (g *gatewayDecorator) SetConditions(conditions []metav1.Condition) {
	g.Status.Conditions = conditions
}

func newGateway() *gatewayDecorator {
	return &gatewayDecorator{
		new(gatewayv1beta1.Gateway),
	}
}

func (g *gatewayDecorator) Clone() *gatewayDecorator {
	return &gatewayDecorator{
		Gateway: g.Gateway.DeepCopy(),
	}
}
