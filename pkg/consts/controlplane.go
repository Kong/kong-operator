package consts

import "github.com/kong/gateway-operator/internal/versions"

// -----------------------------------------------------------------------------
// Consts - DataPlane Generic Parameters
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

// -----------------------------------------------------------------------------
// Consts - Container Parameters
// -----------------------------------------------------------------------------

const (
	// DefaultControlPlaneBaseImage is the base container image that can be used
	// by default for a ControlPlane resource if all other attempts to dynamically
	// decide an image fail.
	DefaultControlPlaneBaseImage = "kong/kubernetes-ingress-controller"

	// DefaultControlPlaneImage is the default container image that can be used if
	// all other attempts to dynamically decide the default image fail.
	DefaultControlPlaneImage = DefaultControlPlaneBaseImage + ":" + versions.DefaultControlPlaneVersion

	// ControlPlaneControllerContainerName is the name of the ingress controller container in a ControlPlane Deployment.
	ControlPlaneControllerContainerName = "controller"

	// DataPlaneInitRetryDelay is the time delay between every attempt (on controller startup)
	// to connect to the Kong Admin API. It needs to be customized to 5 seconds to avoid
	// the ControlPlane crash due to DataPlane slow starts.
	DataPlaneInitRetryDelay = "5s"
)

// TODO: https://github.com/Kong/gateway-operator/issues/1331
// Extract as constants all the Env var Keys used to configure the ControlPlane.
