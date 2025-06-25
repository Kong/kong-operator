package controlplane

// -----------------------------------------------------------------------------
// ControlPlane - Finalizers
// -----------------------------------------------------------------------------

// ControlPlaneFinalizer defines finalizers added by controlplane controller.
type ControlPlaneFinalizer string

const (
	// ControlPlaneFinalizerCleanupValidatingWebhookConfiguration is the finalizer to cleanup validatingwebhookconfigurations owned by controlplane on deleting.
	ControlPlaneFinalizerCleanupValidatingWebhookConfiguration ControlPlaneFinalizer = "gateway-operator.konghq.com/cleanup-validatingwebhookconfiguration"

	// ControlPlaneFinalizerCPInstanceTeardown is the finalizer to tear down a controlplane instance.
	ControlPlaneFinalizerCPInstanceTeardown ControlPlaneFinalizer = "gateway-operator.konghq.com/teardown-cp-instance"
)
