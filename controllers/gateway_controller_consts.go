package controllers

// -----------------------------------------------------------------------------
// Gateway - Finalizers
// -----------------------------------------------------------------------------

// GatewayFinalizer defines finalizers added by gateway controller.
type GatewayFinalizer string

const (
	// GatewayFinalizerCleanupDataPlanes is the finalizer to cleanup owned dataplane resources.
	GatewayFinalizerCleanupDataPlanes GatewayFinalizer = "gateway-operator.konghq.com/cleanup-dataplanes"
	// GatewayFinalizerCleanupControlPlanes is the finalizer to cleanup owned controlplane resources.
	GatewayFinalizerCleanupControlPlanes GatewayFinalizer = "gateway-operator.konghq.com/cleanup-controlplanes"
	// GatewayFinalizerCleanupNetworkpolicies is the finalizer to cleanup owned network policies.
	GatewayFinalizerCleanupNetworkpolicies GatewayFinalizer = "gateway-operator.konghq.com/cleanup-network-policies"
)
