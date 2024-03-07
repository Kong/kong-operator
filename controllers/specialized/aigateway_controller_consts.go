package specialized

// -----------------------------------------------------------------------------
// AIGateway - Finalizers
// -----------------------------------------------------------------------------

// AIGatewayFinalizer defines finalizers added by gateway controller for
// AIGateway resources to ensure proper cleanup of owned resources.
type AIGatewayFinalizer string

const (
	// AIGatewayCleanupFinalizer is a finalizer which indicates that cleanup
	// needs to be processed for an AIGateway resource prior to garbage
	// collection.
	AIGatewayCleanupFinalizer AIGatewayFinalizer = "gateway-operator.konghq.com/aigateway-cleanup"

	// AIGatewayEgressServicePort defines a static Kubernetes endpoint port
	// that can be attached to all Services, HTTPRoutes, and other managed
	// resources in an AIGateway.
	AIGatewayEgressServicePort int = 80
)
