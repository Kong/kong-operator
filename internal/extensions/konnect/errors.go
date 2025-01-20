package konnect

import "errors"

var (
	// ErrCrossNamespaceReference is returned when a Konnect extension references a different namespace.
	ErrCrossNamespaceReference = errors.New("cross-namespace reference is not currently supported for Konnect extensions")
	// ErrKonnectExtensionNotFound is returned when a Konnect extension is not found.
	ErrKonnectExtensionNotFound = errors.New("konnect extension not found")
	// ErrClusterCertificateNotFound is returned when a cluster certificate secret referenced in the KonnectExtension is not found.
	ErrClusterCertificateNotFound = errors.New("cluster certificate not found")
)
