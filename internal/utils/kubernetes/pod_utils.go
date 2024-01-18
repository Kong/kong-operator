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

// SetPodContainer appends a container to the list of containers if it does not exists,
// or it overwrites the existing container, in case it exists.
func SetPodContainer(podSpec *corev1.PodSpec, container *corev1.Container) {
	var found bool
	for i, c := range podSpec.Containers {
		if c.Name == container.Name {
			podSpec.Containers[i] = *container
			found = true
			break
		}
	}
	if !found {
		podSpec.Containers = append(podSpec.Containers, *container)
	}
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
