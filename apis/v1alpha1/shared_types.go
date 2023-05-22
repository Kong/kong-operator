package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// DeploymentOptions is a shared type used on objects to indicate that their
// configuration results in a Deployment which is managed by the Operator and
// includes options for managing Deployments such as the the number of replicas
// or pod options like container image and resource requirements.
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

	// Pods defines the Deployment's pods.
	//
	// +optional
	Pods PodsOptions `json:"pods,omitempty"`
}

// PodOptions is a shared type defining options on Pods deployed as part of
// Deployments managed by the Operator.
type PodsOptions struct {
	// Affinity describes the scheduling rules for the pod.
	//
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Resources describes the compute resource requirements.
	//
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ContainerImage indicates the image that will be used for the Deployment.
	//
	// If omitted a default image will be automatically chosen.
	// In case of DataPlane and ControlPlane CRDs, this is a required field,
	// validated by the admission webhook.
	//
	// +optional
	ContainerImage *string `json:"containerImage,omitempty"`

	// Version indicates the desired version of the ContainerImage.
	//
	// Not available when AutomaticUpgrades is in use.
	//
	// If omitted a default version will be chosen.
	// In case of DataPlane and ControlPlane CRDs, this is a required field,
	// validated by the admission webhook.
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

	// Volumes defines additional volumes to attach to the pods in the Deployment.
	//
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// VolumeMounts defines additional volumes to mount to proxy containers of pods
	// in the Deployment.
	//
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// Labels define labels on Deployment's pods.
	//
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
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
