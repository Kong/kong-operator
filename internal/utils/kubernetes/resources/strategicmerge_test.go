package resources

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/pretty"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

func TestStrategicMergePatchPodTemplateSpec(t *testing.T) {
	makeControlPlaneDeployment := func() (*appsv1.Deployment, error) {
		cp := &v1alpha1.ControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "cp-1",
			},
		}
		return GenerateNewDeploymentForControlPlane(cp, consts.DefaultControlPlaneImage, "kong-sa", "kong-cert-secret")
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
				d, err := makeControlPlaneDeployment()
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
				d, err := makeControlPlaneDeployment()
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
							Name:  "controller",
							Image: "alpine",
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Image = "alpine"
				return d.Spec.Template
			},
		},
		{
			Name: "append a volume and volume mount",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							// NOTE: we need to provide the existing entry in the slice
							// to prevent merging the provided new entry with existing entries.
							Name: "cluster-certificate",
						},
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
							Name: "controller",
							VolumeMounts: []corev1.VolumeMount{
								{
									// NOTE: we need to provide the existing entry in the slice
									// to prevent merging the provided new entry with existing entries.
									Name:      "cluster-certificate",
									MountPath: "/var/cluster-certificate",
								},
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
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "volume1",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: resource.NewQuantity(1000, resource.DecimalSI),
						},
					},
				})
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      "volume1",
					MountPath: "/volume1",
				})

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
							Name: "controller",
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
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{
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
				})
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
							Name: "controller",
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
							// NOTE: we need to provide the existing entry in the slice
							// to prevent merging the provided new entry with existing entries.
							Name: "cluster-certificate",
						},
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
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{
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
				})
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "new_volume",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/host/path",
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
				d, err := makeControlPlaneDeployment()
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
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			d, err := makeControlPlaneDeployment()
			require.NoError(t, err)
			result, err := StrategicMergePatchPodTemplateSpec(&d.Spec.Template, tc.Patch)
			require.NoError(t, err)

			// NOTE: We're using cmp.Diff here because assert.Equal has issues
			// comparing resource.Quantity. We could write custom logic for this
			// but cmp.Diff seems to do the job just fine.
			diff := cmp.Diff(tc.Expected(), *result)
			if !assert.Empty(t, diff) {
				b := bytes.Buffer{}
				require.NoError(t, json.NewEncoder(&b).Encode(result))
				t.Logf("result:\n%s", pretty.Pretty(b.Bytes()))
			}
		})
	}
}
