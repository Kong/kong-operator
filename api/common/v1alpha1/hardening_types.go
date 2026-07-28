package v1alpha1

// HardeningState controls whether the operator applies a hardened
// security context to a DataPlane's proxy container.
//
// +kubebuilder:validation:Enum=enabled;disabled
type HardeningState string

const (
	// HardeningStateEnabled enables the hardened security context.
	HardeningStateEnabled HardeningState = "enabled"
	// HardeningStateDisabled disables the hardened security context (default).
	HardeningStateDisabled HardeningState = "disabled"
)
