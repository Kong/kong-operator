package konnect

import (
	"testing"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// Compile-time assertions that every AI Gateway configuration entity keeps
// implementing the accessor interfaces used by getAPIAuthRef's type switch.
// The switch resolves them via runtime type assertions, so a generated
// accessor signature drifting from the interface would otherwise only fail at
// reconcile time, not at build time.
func TestAIGatewayEntitiesImplementKonnectAIGatewayRefAccessor(t *testing.T) {
	_ = []konnectAIGatewayRefAccessor{
		&aiconfigurationv1alpha1.AIGatewayAgent{},
		&aiconfigurationv1alpha1.AIGatewayAuthStrategy{},
		&aiconfigurationv1alpha1.AIGatewayCACertificate{},
		&aiconfigurationv1alpha1.AIGatewayCertificate{},
		&aiconfigurationv1alpha1.AIGatewayConsumer{},
		&aiconfigurationv1alpha1.AIGatewayConsumerGroup{},
		&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{},
		&aiconfigurationv1alpha1.AIGatewayMCPServer{},
		&aiconfigurationv1alpha1.AIGatewayModel{},
		&aiconfigurationv1alpha1.AIGatewayModelProvider{},
		&aiconfigurationv1alpha1.AIGatewayPolicy{},
		&aiconfigurationv1alpha1.AIGatewaySNI{},
	}
}
