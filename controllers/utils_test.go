package controllers

import (
	"bytes"
	"testing"

	"github.com/bombsimon/logrusr/v3"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	gwtypes "github.com/kong/gateway-operator/internal/types"
)

func Test_ensureContainerImageUpdated(t *testing.T) {
	for _, tt := range []struct {
		name          string
		originalImage string
		newImage      string
		expectedImage string
		updated       bool
		wantErr       string
	}{
		{
			name:          "invalid images produce an error",
			originalImage: "fake:invalid:image:2.7.0",
			newImage:      "kong/kong:2.7.0",
			wantErr:       "invalid container image found: fake:invalid:image:2.7.0",
		},
		{
			name:          "setting new image when existing is local with port is allowed",
			originalImage: "localhost:5000/kic:2.7.0",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "setting new local image is allowed",
			originalImage: "kong/kong:2.7.0",
			newImage:      "localhost:5000/kong:2.7.0",
			expectedImage: "localhost:5000/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "same image and version makes no changes",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       false,
		},
		{
			name:          "version added when not originally present",
			originalImage: "kong/kong",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "version is changed when a new one is provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong:3.0.0",
			expectedImage: "kong/kong:3.0.0",
			updated:       true,
		},
		{
			name:          "image is added when not originally present",
			originalImage: "",
			newImage:      "kong/kong",
			expectedImage: "kong/kong",
			updated:       true,
		},
		{
			name:          "image is changed when a new one is provided",
			originalImage: "kong/kong",
			newImage:      "kong/kong-gateway",
			expectedImage: "kong/kong-gateway",
			updated:       true,
		},
		{
			name:          "image and version are added when not originally present",
			originalImage: "",
			newImage:      "kong/kong-gateway:3.0.0",
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
		{
			name:          "image and version are changed when new ones are provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong-gateway:3.0.0",
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
		{
			name:          "image and version are changed when new ones are provided with local registry",
			originalImage: "kong/kong:2.7.0",
			newImage:      "localhost:5000/kong-gateway:3.0.0",
			expectedImage: "localhost:5000/kong-gateway:3.0.0",
			updated:       true,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			container := generators.NewContainer("test", tt.originalImage, 80)
			updated, err := ensureContainerImageUpdated(&container, tt.newImage)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err.Error())
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.updated, updated)
			if updated {
				assert.NotEqual(t, tt.originalImage, container.Image)
			} else {
				assert.Equal(t, tt.originalImage, container.Image)
			}

			if tt.expectedImage != "" {
				assert.Equal(t, tt.expectedImage, container.Image)
			}
		})
	}
}

func TestLog(t *testing.T) {
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	log := logrusr.New(logger)

	gw := gwtypes.Gateway{}
	t.Run("info logging works both for values and pointers to objects", func(t *testing.T) {
		info(log, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		info(log, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
	})

	t.Run("debug logging works both for values and pointers to objects", func(t *testing.T) {
		debug(log, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		debug(log, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
	})

	t.Run("trace logging works both for values and pointers to objects", func(t *testing.T) {
		trace(log, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		trace(log, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
	})
}

func TestDeploymentOptionsDeepEqual(t *testing.T) {
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
			ret := alphaDeploymentOptionsDeepEqual(tc.o1, tc.o2, tc.envsToIgnore...)
			if tc.expect {
				require.True(t, ret)
			} else {
				require.False(t, ret)
			}
		})
	}
}
