package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestGetPodContainerByName(t *testing.T) {
	for _, tt := range []struct {
		name          string
		pod           corev1.Pod
		containerName string
		expected      *corev1.Container
	}{
		{
			name: "container_found",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			containerName: "test-container",
			expected: &corev1.Container{
				Name:  "test-container",
				Image: "test-image",
			},
		},
		{
			name: "container_not_found",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			containerName: "not-exist-container",
			expected:      nil,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := GetPodContainerByName(&tt.pod.Spec, tt.containerName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetPodContainer(t *testing.T) {
	for _, tt := range []struct {
		name               string
		newContainer       *corev1.Container
		podSpec            *corev1.PodSpec
		expectedContainers []corev1.Container
	}{
		{
			name: "container_is_new_and_is_appended",
			newContainer: &corev1.Container{
				Name:  "test-container4",
				Image: "test-image4",
			},
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container1",
						Image: "test-image1",
					},
					{
						Name:  "test-container2",
						Image: "test-image2",
					},
					{
						Name:  "test-container3",
						Image: "test-image3",
					},
				},
			},
			expectedContainers: []corev1.Container{
				{
					Name:  "test-container1",
					Image: "test-image1",
				},
				{
					Name:  "test-container2",
					Image: "test-image2",
				},
				{
					Name:  "test-container3",
					Image: "test-image3",
				},
				{
					Name:  "test-container4",
					Image: "test-image4",
				},
			},
		},
		{
			name: "container_exists_and_overrides",
			newContainer: &corev1.Container{
				Name:  "test-container3",
				Image: "test-image-new",
			},
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container1",
						Image: "test-image1",
					},
					{
						Name:  "test-container2",
						Image: "test-image2",
					},
					{
						Name:  "test-container3",
						Image: "test-image3",
					},
				},
			},
			expectedContainers: []corev1.Container{
				{
					Name:  "test-container1",
					Image: "test-image1",
				},
				{
					Name:  "test-container2",
					Image: "test-image2",
				},
				{
					Name:  "test-container3",
					Image: "test-image-new",
				},
			},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			SetPodContainer(tt.podSpec, tt.newContainer)
			assert.Equal(t, tt.expectedContainers, tt.podSpec.Containers)
		})
	}
}

func TestGetPodVolumeByName(t *testing.T) {
	for _, tt := range []struct {
		name       string
		pod        corev1.Pod
		volumeName string
		expected   *corev1.Volume
	}{
		{
			name: "volume_found",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "test-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			volumeName: "test-volume",
			expected: &corev1.Volume{
				Name: "test-volume",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
		{
			name: "volume_not_found",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "test-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			volumeName: "not-exist-volume",
			expected:   nil,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := GetPodVolumeByName(&tt.pod.Spec, tt.volumeName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetContainerVolumeMountByMountPath(t *testing.T) {
	for _, tt := range []struct {
		name      string
		container corev1.Container
		mountPath string
		expected  *corev1.VolumeMount
	}{
		{
			name: "volume_mount_found",
			container: corev1.Container{
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "test-mount1",
						MountPath: "/test/path1",
					},
					{
						Name:      "test-mount2",
						MountPath: "/test/path2",
					},
					{
						Name:      "test-mount3",
						MountPath: "/test/path3",
					},
				},
			},
			mountPath: "/test/path2",
			expected: &corev1.VolumeMount{
				Name:      "test-mount2",
				MountPath: "/test/path2",
			},
		},
		{
			name: "volume_mount_doesnt_exist",
			container: corev1.Container{
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "test-mount1",
						MountPath: "/test/path1",
					},
					{
						Name:      "test-mount2",
						MountPath: "/test/path2",
					},
					{
						Name:      "test-mount3",
						MountPath: "/test/path3",
					},
				},
			},
			mountPath: "/test/path4",
			expected:  nil,
		},
		{
			name: "volume_mount_doesnt_exist",
			container: corev1.Container{
				VolumeMounts: []corev1.VolumeMount{},
			},
			mountPath: "/not/exist/path",
			expected:  nil,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := GetContainerVolumeMountByMountPath(&tt.container, tt.mountPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}
