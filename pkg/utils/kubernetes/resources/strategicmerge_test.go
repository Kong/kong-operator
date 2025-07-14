package resources

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/pretty"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/pkg/consts"
)

func TestStrategicMergePatchPodTemplateSpec(t *testing.T) {
	makeDataPlaneDeployment := func() (*appsv1.Deployment, error) {
		dp := &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "dp-1",
			},
		}
		d, err := GenerateNewDeploymentForDataPlane(dp, consts.DefaultDataPlaneImage)
		if err != nil {
			return nil, err
		}
		d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env,
			[]corev1.EnvVar{
				{
					Name:  "ENV1",
					Value: "VALUE1",
				},
				{
					Name:  "ENV2",
					Value: "VALUE2",
				},
				{
					Name:  "ENV3",
					Value: "VALUE3",
				},
			}...,
		)

		return d.Unwrap(), nil
	}

	testcases := []struct {
		Name     string
		Patch    *corev1.PodTemplateSpec
		Expected func() corev1.PodTemplateSpec
	}{
		{
			Name:  "empty patch doesn't change anything",
			Patch: &corev1.PodTemplateSpec{},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				return d.Spec.Template
			},
		},
		{
			Name: "add pod labels",
			Patch: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"label1": "value1",
						"label2": "value2",
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Labels["label1"] = "value1"
				d.Spec.Template.Labels["label2"] = "value2"
				return d.Spec.Template
			},
		},
		{
			Name: "patch the controller image",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  consts.DataPlaneProxyContainerName,
							Image: "alpine",
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Image = "alpine"
				d.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways
				return d.Spec.Template
			},
		},
		{
			Name: "add env vars",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									// Prepend
									Name:  "KONG_ENV",
									Value: "random-value",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "KONG_ENV",
						Value: "random-value",
					},
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "overwrite env vars",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "custom-value",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "custom-value",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "overwrite and add env vars",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "custom-value",
								},
								{
									Name:  "CUSTOM_ENV",
									Value: "custom-env-value",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "custom-value",
					},
					{
						Name:  "CUSTOM_ENV",
						Value: "custom-env-value",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "add and overwrite env vars",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "CUSTOM_ENV",
									Value: "custom-env-value",
								},
								{
									Name:  "ENV1",
									Value: "custom-value",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "CUSTOM_ENV",
						Value: "custom-env-value",
					},
					{
						Name:  "ENV1",
						Value: "custom-value",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "append a volume and volume mount",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "volume1",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									SizeLimit: resource.NewQuantity(1000, resource.DecimalSI),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume1",
									MountPath: "/volume1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes,
					corev1.Volume{
						Name: "volume1",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{
								SizeLimit: resource.NewQuantity(1000, resource.DecimalSI),
							},
						},
					})
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts,
					corev1.VolumeMount{
						Name:      "volume1",
						MountPath: "/volume1",
					},
				)

				return d.Spec.Template
			},
		},
		{
			Name: "append a sidecar",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							// NOTE: we need to provide the existing entry in the slice
							// to prevent merging the provided new entry with existing entries.
							Name: consts.DataPlaneProxyContainerName,
						},
						{
							Name:  "sidecar",
							Image: "alpine",
							Command: []string{
								"sleep", "1000",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				sidecarContainer := corev1.Container{
					Name:  "sidecar",
					Image: "alpine",
					Command: []string{
						"sleep", "1000",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "ENV1",
							Value: "VALUE1",
						},
					},
				}
				SetDefaultsContainer(&sidecarContainer)
				d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, sidecarContainer)
				return d.Spec.Template
			},
		},
		{
			Name: "append a sidecar and a volume mount",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							// NOTE: we need to provide the existing entry in the slice
							// to prevent merging the provided new entry with existing entries.
							Name: consts.DataPlaneProxyContainerName,
						},
						{
							Name:  "sidecar",
							Image: "alpine",
							Command: []string{
								"sleep", "1000",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "new_volume",
									MountPath: "/volume",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "new_volume",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/host/path",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				sidecarContainer := corev1.Container{
					Name:  "sidecar",
					Image: "alpine",
					Command: []string{
						"sleep", "1000",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "ENV1",
							Value: "VALUE1",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "new_volume",
							MountPath: "/volume",
						},
					},
				}
				SetDefaultsContainer(&sidecarContainer)
				d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, sidecarContainer)
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "new_volume",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/host/path",
							Type: lo.ToPtr(corev1.HostPathUnset),
						},
					},
				})
				return d.Spec.Template
			},
		},
		{
			Name: "change affinity",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"app": "random_app",
										},
									},
								},
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "metadata.name",
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													"abc",
													"def",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Affinity = &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "random_app",
									},
								},
							},
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "metadata.name",
											Operator: metav1.LabelSelectorOpIn,
											Values: []string{
												"abc",
												"def",
											},
										},
									},
								},
							},
						},
					},
				}

				return d.Spec.Template
			},
		},
		{
			Name: "append a secret volume and volume mount",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							// NOTE: we need to provide the existing entry in the slice
							// to prevent merging the provided new entry with existing entries.
							Name: consts.DataPlaneProxyContainerName,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "new_volume",
									MountPath: "/new_volume",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "new_volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "secret-1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				volume := corev1.Volume{
					Name: "new_volume",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "secret-1",
						},
					},
				}
				SetDefaultsVolume(&volume)
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, volume)
				require.Len(t, d.Spec.Template.Spec.Containers, 1)
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts,
					corev1.VolumeMount{
						Name:      "new_volume",
						MountPath: "/new_volume",
					})
				return d.Spec.Template
			},
		},
		{
			Name: "add hostPath volume and volumeMount",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "hostpath-volumemount",
									MountPath: "/var/log/hostpath",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "hostpath-volume",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/log",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "hostpath-volume",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/var/log",
								Type: lo.ToPtr(corev1.HostPathUnset),
							},
						},
					},
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(
					d.Spec.Template.Spec.Containers[0].VolumeMounts,
					corev1.VolumeMount{
						Name:      "hostpath-volumemount",
						MountPath: "/var/log/hostpath",
					},
				)

				return d.Spec.Template
			},
		},
		{
			Name: "add envs with fieldRef",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name: "LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name: "LIMIT",
						// NOTE: this is an artifact of the strategic merge patch at work.
						// This values comes from the first entry in the base patch.
						// In order to overcome this we need to re-state the base values
						// in the slice.
						Value: "VALUE1",
						ValueFrom: &corev1.EnvVarSource{
							ResourceFieldRef: &corev1.ResourceFieldSelector{
								Resource: "limits.cpu",
								Divisor:  *resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}

				return d.Spec.Template
			},
		},
		{
			Name: "add envs with fieldRef re-stating the base",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
								{
									Name:  "ENV2",
									Value: "VALUE2",
								},
								{
									Name:  "ENV3",
									Value: "VALUE3",
								},
								// We're moving the `LIMIT` entry from the 1st to the 4th place in the slice to make sure it will not end up with both `Value` and `ValueFrom` fields.
								{
									Name: "LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeDataPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
					{
						Name: "LIMIT",
						ValueFrom: &corev1.EnvVarSource{
							ResourceFieldRef: &corev1.ResourceFieldSelector{
								Resource: "limits.cpu",
								Divisor:  *resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				}

				return d.Spec.Template
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Name, func(t *testing.T) {
			d, err := makeDataPlaneDeployment()
			require.NoError(t, err)
			result, err := StrategicMergePatchPodTemplateSpec(&d.Spec.Template, tc.Patch)
			require.NoError(t, err)

			// NOTE: We're using cmp.Diff here because assert.Equal has issues
			// comparing resource.Quantity. We could write custom logic for this
			// but cmp.Diff seems to do the job just fine.
			diff := cmp.Diff(*result, tc.Expected())
			if !assert.Empty(t, diff) {
				b := bytes.Buffer{}
				require.NoError(t, json.NewEncoder(&b).Encode(result))
				t.Logf("result:\n%s", pretty.Pretty(b.Bytes()))
			}
		})
	}
}

func TestSetDefaultsPodTemplateSpec(t *testing.T) {
	testcases := []struct {
		Name     string
		Patch    *corev1.PodTemplateSpec
		Expected corev1.PodTemplateSpec
	}{
		{
			Name: "serivce account name is copied to deprecated field",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "account",
				},
			},
			Expected: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName:       "account",
					DeprecatedServiceAccount: "account",
					// NOTE: below set fields are irrelevant for the test
					// but are set by SetDefaultsPodTemplateSpec regardless.
					RestartPolicy:                 corev1.RestartPolicyAlways,
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 corev1.DefaultSchedulerName,
					TerminationGracePeriodSeconds: lo.ToPtr(int64(30)),
					SecurityContext:               &corev1.PodSecurityContext{},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Name, func(t *testing.T) {
			SetDefaultsPodTemplateSpec(tc.Patch)
			assert.Equal(t, tc.Expected, *tc.Patch)
		})
	}
}
