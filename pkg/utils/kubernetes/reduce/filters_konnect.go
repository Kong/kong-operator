package reduce

import (
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// -----------------------------------------------------------------------------
// Filter functions - KongDataPlaneClientCertificate
// -----------------------------------------------------------------------------

// FilterKongDataPlaneClientCertificates filters out the KongDataPlaneClientCertificates to be kept and returns all
// the KongDataPlaneClientCertificates to be deleted.
// The filtered-out KongDataPlaneClientCertificates is decided as follows:
// 1. creationTimestamp (older is better)
func FilterKongDataPlaneClientCertificates(certs []configurationv1alpha1.KongDataPlaneClientCertificate) []configurationv1alpha1.KongDataPlaneClientCertificate {
	if len(certs) < 2 {
		return []configurationv1alpha1.KongDataPlaneClientCertificate{}
	}

	best := 0
	for i, cert := range certs {
		if cert.CreationTimestamp.Before(&certs[best].CreationTimestamp) {
			best = i
		}
	}

	return append(certs[:best], certs[best+1:]...)
}
