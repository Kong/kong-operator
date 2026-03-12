package v1alpha1

// GatewayDescription A human-readable description of the Gateway.
type GatewayDescription string

// GatewayName The name of the Gateway.
type GatewayName string

// Labels Labels store metadata of an entity that can be used for filtering an
// entity list or for searching across entity types.
//
// Keys must be of length 1-63 characters, and cannot start with "kong",
// "konnect", "mesh", "kic", or "_".
//
type Labels map[string]string

// LabelsUpdate Labels store metadata of an entity that can be used for
// filtering an entity list or for searching across entity types.
//
// Labels are intended to store **INTERNAL** metadata.
//
// Keys must be of length 1-63 characters, and cannot start with "kong",
// "konnect", "mesh", "kic", or "_".
//
type LabelsUpdate map[string]string

// MinRuntimeVersion The minimum runtime version supported by the API.
// This is the lowest version of the data plane
// release that can be used with the entity model.
// When not specified, the minimum runtime version will be pinned to the latest
// available release.
//
type MinRuntimeVersion string

