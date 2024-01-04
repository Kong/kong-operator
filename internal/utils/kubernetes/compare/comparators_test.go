package compare

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

func TestDeploymentOptionsV1AlphaDeepEqual(t *testing.T) {
	const (
		containerName = "controller"
	)

	testcases := []struct {
		name         string
		o1, o2       *operatorv1alpha1.DeploymentOptions
		envsToIgnore []string
		expect       bool
	}{
		{
			name:   "nils are equal",
			expect: true,
		},
		{
			name:   "empty values are equal",
			o1:     &operatorv1alpha1.DeploymentOptions{},
			o2:     &operatorv1alpha1.DeploymentOptions{},
			expect: true,
		},
		{
			name: "different resource requirements implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different pod labels implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "v",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different image implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different env var implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "the same",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: true,
		},
		{
			name: "different replicas implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				Replicas: lo.ToPtr(int32(1)),
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				Replicas: lo.ToPtr(int32(3)),
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different env vars but included in the vars to ignore implies equal opts",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
							},
						},
					},
				},
			},
			envsToIgnore: []string{"KONG_TEST_VAR"},
			expect:       true,
		},
		{
			name: "different env vars with 1 one them included in the vars to ignore implies unequal opts",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
									{
										Name:  "KONG_TEST_VAR_2",
										Value: "VALUE2",
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
							},
						},
					},
				},
			},
			envsToIgnore: []string{"KONG_TEST_VAR"},
			expect:       false,
		},
		{
			name: "different labels unequal opts",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "a",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "a",
							"b": "b",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
							},
						},
					},
				},
			},
			envsToIgnore: []string{"KONG_TEST_VAR"},
			expect:       false,
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ret := DeploymentOptionsV1AlphaDeepEqual(tc.o1, tc.o2, tc.envsToIgnore...)
			if tc.expect {
				require.True(t, ret)
			} else {
				require.False(t, ret)
			}
		})
	}
}
