package versions

// VersionValidationOption is the function signature to be used
// as option to validate ControlPlane and DataPlane versions
type VersionValidationOption func(version string) bool
