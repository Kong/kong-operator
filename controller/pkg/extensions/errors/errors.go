package errors

import "errors"

var (
	// ErrCrossNamespaceReference is returned when a Konnect extension references a different namespace.
	ErrCrossNamespaceReference = errors.New("cross-namespace reference is not currently supported for Konnect extensions")
	// ErrKonnectExtensionNotFound is returned when a Konnect extension is not found.
	ErrKonnectExtensionNotFound = errors.New("konnect extension not found")
	// ErrClusterCertificateNotFound is returned when a cluster certificate secret referenced in the KonnectExtension is not found.
	ErrClusterCertificateNotFound = errors.New("cluster certificate not found")
	// ErrKonnectExtensionNotReady is returned when a Konnect extension is not ready.
	ErrKonnectExtensionNotReady = errors.New("konnect extension is not ready")
	// ErrInvalidExtensions is returned when an invalid extension is referenced.
	ErrInvalidExtensions = errors.New("invalid extensions")
)

// IsKonnectExtensionError returns true if the error is a Konnect extension error.
func IsKonnectExtensionError(err error) bool {
	return errors.Is(err, ErrCrossNamespaceReference) ||
		errors.Is(err, ErrKonnectExtensionNotFound) ||
		errors.Is(err, ErrClusterCertificateNotFound) ||
		errors.Is(err, ErrKonnectExtensionNotReady) ||
		errors.Is(err, ErrInvalidExtensions)
}
