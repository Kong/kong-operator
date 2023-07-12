package resources

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

func TestGenerateNewDeploymentForDataPlane(t *testing.T) {
	const (
		certSecretName = "cert-secret-name"
		dataplaneImage = "kong:3.0"
	)

	tests := []struct {
		name      string
		dataplane *operatorv1alpha1.DataPlane
		testFunc  func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec)
	}{
		{
			name: "without resources specified we get the defaults",
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "1",
					Namespace: "test-namespace",
				},
			},
			testFunc: func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec) {
				require.Len(t, deploymentSpec.Template.Spec.Containers, 1)
				container := deploymentSpec.Template.Spec.Containers[0]
				expectedResources := *DefaultDataPlaneResources()
				if !ResourceRequirementsEqual(expectedResources, &container.Resources) {
					require.Equal(t, expectedResources, container.Resources)
				}
			},
		},
		{
			name: "with CPU resources specified",
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "1",
					Namespace: "test-namespace",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Resources: &corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("100m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("1"),
										},
									},
								},
							},
						},
					},
				},
			},
			testFunc: func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec) {
				require.Len(t, deploymentSpec.Template.Spec.Containers, 1)
				container := deploymentSpec.Template.Spec.Containers[0]
				expectedResources := corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse(DefaultDataPlaneMemoryRequest),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse(DefaultDataPlaneMemoryLimit),
					},
				}
				if !ResourceRequirementsEqual(expectedResources, &container.Resources) {
					require.Equal(t, expectedResources, container.Resources)
				}
			},
		},
		{
			name: "with Memory resources specified",
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "1",
					Namespace: "test-namespace",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Resources: &corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("256Mi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("1024Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			testFunc: func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec) {
				require.Len(t, deploymentSpec.Template.Spec.Containers, 1)
				container := deploymentSpec.Template.Spec.Containers[0]
				expectedResources := corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(DefaultDataPlaneCPURequest),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(DefaultDataPlaneCPULimit),
						corev1.ResourceMemory: resource.MustParse("1024Mi"),
					},
				}
				if !ResourceRequirementsEqual(expectedResources, &container.Resources) {
					require.Equal(t, expectedResources, container.Resources)
				}
			},
		},
		{
			name: "with Pod labels specified",
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-name",
					Namespace: "test-namespace",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Resources: &corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("256Mi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("1024Mi"),
										},
									},
									Labels: map[string]string{
										"label-a": "value-a",
									},
								},
							},
						},
					},
				},
			},
			testFunc: func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec) {
				require.Equal(t,
					map[string]string{
						"app":     "dataplane-name",
						"label-a": "value-a",
					},
					deploymentSpec.Template.Labels,
				)
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			deployment := GenerateNewDeploymentForDataPlane(tt.dataplane, dataplaneImage, certSecretName)
			tt.testFunc(t, &deployment.Spec)
		})
	}
}
