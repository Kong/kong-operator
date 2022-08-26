package resources

import (
	corev1 "k8s.io/api/core/v1"
)

// GetPodContainerByName takes a PodSpec reference and a string and returns a reference to the container in the PodSpec
// with that name, if any exists.
func GetPodContainerByName(podSpec *corev1.PodSpec, name string) *corev1.Container {
	for i, container := range podSpec.Containers {
		if container.Name == name {
			return &podSpec.Containers[i]
		}
	}
	return nil
}
