package v1alpha1

// LabelsUpdate Labels store metadata of an entity that can be used for
// filtering an entity list or for searching across entity types.
//
// Labels are intended to store **INTERNAL** metadata.
//
// Keys must be of length 1-63 characters, and cannot start with "kong",
// "konnect", "mesh", "kic", or "_".
type LabelsUpdate map[string]string
