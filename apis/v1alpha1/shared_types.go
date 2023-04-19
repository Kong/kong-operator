package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// DeploymentOptions is a shared type used on objects to indicate that their
// configuration results in a Deployment which is managed by the Operator and
// includes options for managing Deployments such as the container image and
// version, as well as Env variable overrides.
type DeploymentOptions struct {
	// Replicas describes the number of desired pods.
	// This is a pointer to distinguish between explicit zero and not specified.
	// This only affects the DataPlane deployments for now, for more details on
	// ControlPlane scaling please see https://github.com/Kong/gateway-operator/issues/736.
	//
	// +optional
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources describes the compute resource requirements.
	//
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ContainerImage indicates the image that will be used for the Deployment.
	//
	// If omitted a default image will be automatically chosen.
	//
	// +optional
	ContainerImage *string `json:"containerImage,omitempty"`

	// Version indicates the desired version of the ContainerImage.
	//
	// Not available when AutomaticUpgrades is in use.
	//
	// If omitted a default version will be chosen.
	//
	// +optional
	Version *string `json:"version,omitempty"`

	// Env indicates the environment variables to set for the Deployment.
	//
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom indicates the environment variables to be set for the Deployment
	// with the values set from specific sources (such as Secrets).
	//
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
}

// GatewayConfigurationTargetKind is an object kind that can be targeted for
// GatewayConfiguration attachment.
type GatewayConfigurationTargetKind string

const (
	// GatewayConfigurationTargetKindGateway is a target kind which indicates
	// that a Gateway resource is the target.
	GatewayConfigurationTargetKindGateway GatewayConfigurationTargetKind = "Gateway"

	// GatewayConfigurationTargetKindGatewayClass is a target kind which indicates
	// that a GatewayClass resource is the target.
	GatewayConfigurationTargetKindGatewayClass GatewayConfigurationTargetKind = "GatewayClass"
)
