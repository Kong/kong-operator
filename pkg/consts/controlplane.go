package consts

// -----------------------------------------------------------------------------
// Consts - ControlPlane Generic Parameters
// -----------------------------------------------------------------------------

const (
	// ControlPlanePrefix is used as a name prefix to generate controlplane-owned objects' name.
	ControlPlanePrefix = "controlplane"
)

// -----------------------------------------------------------------------------
// Consts - ControlPlane Labels and Annotations
// -----------------------------------------------------------------------------

const (
	// ControlPlaneManagedLabelValue indicates that an object's lifecycle is managed
	// by the controlplane controller.
	ControlPlaneManagedLabelValue = "controlplane"
)
