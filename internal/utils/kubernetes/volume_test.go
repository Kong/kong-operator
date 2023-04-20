package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestHasSameVolumeSource(t *testing.T) {
	testCases := []struct {
		name                 string
		baseVolumeSource     *corev1.VolumeSource
		comparedVolumeSource *corev1.VolumeSource
		equal                bool
	}{
		{
			name:                 "nil volume sources should be equal",
			baseVolumeSource:     nil,
			comparedVolumeSource: nil,
			equal:                true,
		},
		{
			name:                 "nil volumes should not be equal to non-nil volume",
			baseVolumeSource:     &corev1.VolumeSource{},
			comparedVolumeSource: nil,
			equal:                false,
		},
		{
			name:                 "empty volume sources should be equal",
			baseVolumeSource:     &corev1.VolumeSource{},
			comparedVolumeSource: &corev1.VolumeSource{},
			equal:                true,
		},
		{
			name: "empty volume source and non-empty secret volume should not be eqaul",
			baseVolumeSource: &corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "secret0",
				},
			},
			comparedVolumeSource: &corev1.VolumeSource{},
			equal:                false,
		},
		{
			name: "secret volumes with same secret name should be eqaul",
			baseVolumeSource: &corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "secret0",
				},
			},
			comparedVolumeSource: &corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  "secret0",
					DefaultMode: new(int32),
				},
			},
			equal: true,
		},
		{
			name: "secret volumes with different secret name should not be eqaul",
			baseVolumeSource: &corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "secret0",
				},
			},
			comparedVolumeSource: &corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "secret1",
				},
			},
			equal: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			equal := HasSameVolumeSource(tc.baseVolumeSource, tc.comparedVolumeSource)
			if tc.equal {
				require.True(t, equal, "volume sources should be considered equal")
			} else {
				require.False(t, equal, "volume sources should not be considered equal")
			}
		})
	}
}
