package kubernetes

// This file includes utility functions for operating `Volume`
// resources in kubernetes.
import (
	corev1 "k8s.io/api/core/v1"
)

// HasSameVolumeSource returns true if the two volume sources are the same
// and we do not need to update the volume in deployments.
// currently it can only compare secrets.
func HasSameVolumeSource(baseVolumeSource, comparedVolumeSource *corev1.VolumeSource) bool {
	// return true if volume sources are both nil; false if only one of them is nil.
	if baseVolumeSource == nil {
		return comparedVolumeSource == nil
	}
	if comparedVolumeSource == nil {
		return false
	}

	// if they are both non-nil, compare the actual content.
	// TODO: compare ALL supported volume sources in k8s API:
	// https://github.com/Kong/gateway-operator/issues/703

	// compare for secret volumes.
	if baseVolumeSource.Secret != nil {
		if comparedVolumeSource.Secret == nil {
			return false
		}

		return baseVolumeSource.Secret.SecretName == comparedVolumeSource.Secret.SecretName
	}

	return true
}
