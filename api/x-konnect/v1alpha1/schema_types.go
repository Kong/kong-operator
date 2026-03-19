package v1alpha1

// GatewayDescription A human-readable description of the Gateway.
type GatewayDescription string

// GatewayName The name of the Gateway.
type GatewayName string

// LabelsValue is the value type for Labels.
//
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:Pattern=`^[a-z0-9A-Z]{1}([a-z0-9A-Z-._]*[a-z0-9A-Z]+)?$`
type LabelsValue string

// Labels store metadata of an entity that can be used for filtering an entity
// list or for searching across entity types.
//
// Keys must be of length 1-63 characters, and cannot start with "kong",
// "konnect", "mesh", "kic", or "_".
type Labels map[string]LabelsValue

// LabelsUpdateValue is the value type for LabelsUpdate.
//
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:Pattern=`^[a-z0-9A-Z]{1}([a-z0-9A-Z-._]*[a-z0-9A-Z]+)?$`
type LabelsUpdateValue string

// LabelsUpdate Labels store metadata of an entity that can be used for
// filtering an entity list or for searching across entity types.
//
// Labels are intended to store **INTERNAL** metadata.
//
// Keys must be of length 1-63 characters, and cannot start with "kong",
// "konnect", "mesh", "kic", or "_".
type LabelsUpdate map[string]LabelsUpdateValue

// MinRuntimeVersion The minimum runtime version supported by the API.
// This is the lowest version of the data plane
// release that can be used with the entity model.
// When not specified, the minimum runtime version will be pinned to the latest
// available release.
type MinRuntimeVersion string
