package config

import (
	"maps"
	"sort"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestFillDataPlaneProxyContainerEnvs(t *testing.T) {
	toSortedSlice := func(envVars map[string]string) []corev1.EnvVar {
		ret := lo.MapToSlice(envVars, func(k, v string) corev1.EnvVar {
			return corev1.EnvVar{
				Name:  k,
				Value: v,
			}
		})
		sort.Sort(k8sutils.SortableEnvVars(ret))
		return ret
	}

	t.Run("nil doesn't panic", func(t *testing.T) {
		FillContainerEnvs(nil, nil, consts.DataPlaneProxyContainerName, EnvVarMapToSlice(KongDefaults))
	})

	testcases := []struct {
		name            string
		podTemplateSpec *corev1.PodTemplateSpec
		expected        []corev1.EnvVar
		existing        []corev1.EnvVar
	}{
		{
			name: "new env gest added to defaults",
			podTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "TEST_1",
									Value: "VALUE_1",
								},
							},
						},
					},
				},
			},
			expected: func() []corev1.EnvVar {
				m := maps.Clone(KongDefaults)
				m["TEST_1"] = "VALUE_1"
				ret := toSortedSlice(m)
				sort.Sort(k8sutils.SortableEnvVars(ret))
				return ret
			}(),
		},
		{
			name: "override is respected",
			podTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "KONG_ADMIN_ACCESS_LOG",
									Value: "/dev/null",
								},
							},
						},
					},
				},
			},
			expected: func() []corev1.EnvVar {
				m := maps.Clone(KongDefaults)
				m["KONG_ADMIN_ACCESS_LOG"] = "/dev/null"
				ret := toSortedSlice(m)
				sort.Sort(k8sutils.SortableEnvVars(ret))
				return ret
			}(),
		},
		{
			name: "existing with no overrides persist",
			podTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env:  []corev1.EnvVar{},
						},
					},
				},
			},
			existing: func() []corev1.EnvVar {
				m := maps.Clone(KongDefaults)
				m["RED"] = "RED"
				m["BLUE"] = "BLUE"
				ret := toSortedSlice(m)
				sort.Sort(k8sutils.SortableEnvVars(ret))
				return ret
			}(),
			expected: func() []corev1.EnvVar {
				m := maps.Clone(KongDefaults)
				m["RED"] = "RED"
				m["BLUE"] = "BLUE"
				ret := toSortedSlice(m)
				sort.Sort(k8sutils.SortableEnvVars(ret))
				return ret
			}(),
		},
		{
			name: "existing with overrides overidden",
			podTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "BLUE",
									Value: "OVERRIDE",
								},
							},
						},
					},
				},
			},
			existing: func() []corev1.EnvVar {
				m := maps.Clone(KongDefaults)
				m["RED"] = "RED"
				m["BLUE"] = "BLUE"
				ret := toSortedSlice(m)
				sort.Sort(k8sutils.SortableEnvVars(ret))
				return ret
			}(),
			expected: func() []corev1.EnvVar {
				m := maps.Clone(KongDefaults)
				m["RED"] = "RED"
				m["BLUE"] = "OVERRIDE"
				ret := toSortedSlice(m)
				sort.Sort(k8sutils.SortableEnvVars(ret))
				return ret
			}(),
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			FillContainerEnvs(tc.existing, tc.podTemplateSpec, consts.DataPlaneProxyContainerName, EnvVarMapToSlice(KongDefaults))
			container := k8sutils.GetPodContainerByName(&tc.podTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
			require.Equal(t, tc.expected, container.Env)
		})
	}
}
