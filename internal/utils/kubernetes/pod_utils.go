package kubernetes

// This file includes utility functions for operating `Pod`
// resources in kubernetes.
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

// GetPodVolumeByName gets the pointer of volume with given name.
// if the volume with given name does not exist in the pod, it returns `nil`.
func GetPodVolumeByName(podSpec *corev1.PodSpec, name string) *corev1.Volume {
	for i, volume := range podSpec.Volumes {
		if volume.Name == name {
			return &podSpec.Volumes[i]
		}
	}
	return nil
}

// GetContainerVolumeMountByPath gets the volume mounted to given path in container.
// if the mount path does not exist, it returns `nil`.
func GetContainerVolumeMountByMountPath(container *corev1.Container, mountPath string) *corev1.VolumeMount {
	for i, volumeMount := range container.VolumeMounts {
		if volumeMount.MountPath == mountPath {
			return &container.VolumeMounts[i]
		}
	}

	return nil
}
