package consts

import "github.com/kong/kong-operator/internal/versions"

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

// -----------------------------------------------------------------------------
// Consts - ControlPlane webhook-related parameters
// -----------------------------------------------------------------------------

const (
	// ControlPlaneAdmissionWebhookPortName is the name of the port on which the control plane admission webhook listens.
	ControlPlaneAdmissionWebhookPortName = "webhook"
	// ControlPlaneAdmissionWebhookListenPort is the port on which the control plane admission webhook listens.
	ControlPlaneAdmissionWebhookListenPort = 8080
	// ControlPlaneAdmissionWebhookEnvVarValue is the default value for the admission webhook env var.
	ControlPlaneAdmissionWebhookEnvVarValue = "0.0.0.0:8080"
	// ControlPlaneAdmissionWebhookVolumeName is the name of the volume that holds the certificate that's used
	// for serving the admission webhook in control plane.
	ControlPlaneAdmissionWebhookVolumeName = "admission-webhook-certificate"
	// ControlPlaneAdmissionWebhookVolumeMountPath is the path where the admission webhook certificate will be mounted.
	ControlPlaneAdmissionWebhookVolumeMountPath = "/admission-webhook"
)

// TODO: https://github.com/kong/kong-operator/issues/141
// Extract as constants all the Env var Keys used to configure the ControlPlane.
