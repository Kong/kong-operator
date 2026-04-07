package volumes

import (
	corev1 "k8s.io/api/core/v1"
)

// GetByName returns a pointer to the Volume with the given name from the
// list of volumes, or nil if no such volume exists.
func GetByName(volumes []corev1.Volume, name string) *corev1.Volume {
	for _, v := range volumes {
		if v.Name == name {
			return v.DeepCopy()
		}
	}
	return nil
}

// GetMountsByVolumeName returns a slice of VolumeMounts that have the given name.
func GetMountsByVolumeName(volumeMounts []corev1.VolumeMount, name string) []corev1.VolumeMount {
	ret := make([]corev1.VolumeMount, 0)
	for _, m := range volumeMounts {
		if m.Name == name {
			ret = append(ret, m)
		}
	}
	return ret
}
