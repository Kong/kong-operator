package resources

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
)

func TestGenerateNewDeploymentForDataPlane(t *testing.T) {
	const (
		certSecretName = "cert-secret-name"
		dataplaneImage = "kong:3.0"
	)

	tests := []struct {
		name      string
		dataplane *operatorv1beta1.DataPlane
		testFunc  func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec)
	}{
		{
			name: "without resources specified we get the defaults",
			dataplane: &operatorv1beta1.DataPlane{
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
				if !ResourceRequirementsEqual(expectedResources, container.Resources) {
					require.Equal(t, expectedResources, container.Resources)
				}
			},
		},
		{
			name: "with CPU resources specified we get merged resources",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "1",
					Namespace: "test-namespace",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: consts.DataPlaneProxyContainerName,
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("100m"),
												},
												Limits: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("1000m"),
												},
											},
										}},
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
				expectedResources := *DefaultDataPlaneResources()

				// templated data gets merged on top of the defaults, verify that
				expectedResources.Requests["cpu"] = resource.MustParse("100m")
				expectedResources.Limits["cpu"] = resource.MustParse("1000m")

				if !ResourceRequirementsEqual(expectedResources, container.Resources) {
					require.Equal(t, expectedResources, container.Resources)
				}
			},
		},
		{
			name: "with Memory resources specified",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "1",
					Namespace: "test-namespace",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: consts.DataPlaneProxyContainerName,
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceMemory: resource.MustParse("2Gi"),
												},
												Limits: corev1.ResourceList{
													corev1.ResourceMemory: resource.MustParse("4Gi"),
												},
											},
										}},
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
				expectedResources := *DefaultDataPlaneResources()

				// templated data gets merged on top of the defaults, verify that
				expectedResources.Requests["memory"] = resource.MustParse("2Gi")
				expectedResources.Limits["memory"] = resource.MustParse("4Gi")

				if !ResourceRequirementsEqual(expectedResources, container.Resources) {
					require.Equal(t, expectedResources, container.Resources)
				}
			},
		},
		// NOTE: Currently, the generation code doesn't code doesn't apply any
		// patches. This is now handled in &StrategicMergePatchPodTemplateSpec()
		// hence the tests here are mostly unnecessary. Leaving one as a sanity check.
		{
			name: "with Pod labels specified",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-name",
					Namespace: "test-namespace",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									ObjectMeta: metav1.ObjectMeta{
										Labels: map[string]string{
											"label-a": "value-a",
										},
									},
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceMemory: resource.MustParse("256Mi"),
												},
												Limits: corev1.ResourceList{
													corev1.ResourceMemory: resource.MustParse("1024Mi"),
												},
											},
										}},
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
		{
			name: "with Affinity specified",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-name",
					Namespace: "test-namespace",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Affinity: &corev1.Affinity{
											NodeAffinity: &corev1.NodeAffinity{
												RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
													NodeSelectorTerms: []corev1.NodeSelectorTerm{
														{
															MatchFields: []corev1.NodeSelectorRequirement{
																{
																	Key:      "topology.kubernetes.io/zone",
																	Operator: corev1.NodeSelectorOpIn,
																	Values: []string{
																		"europe-west-1",
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
							},
						},
					},
				},
			},
			testFunc: func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec) {
				expected := &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchFields: []corev1.NodeSelectorRequirement{
										{
											Key:      "topology.kubernetes.io/zone",
											Operator: corev1.NodeSelectorOpIn,
											Values: []string{
												"europe-west-1",
											},
										},
									},
								},
							},
						},
					},
				}
				actual := deploymentSpec.Template.Spec.Affinity
				require.Equal(t, expected, actual)
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			deployment, err := GenerateNewDeploymentForDataPlane(tt.dataplane, dataplaneImage, certSecretName)
			require.NoError(t, err)
			tt.testFunc(t, &deployment.Spec)
		})
	}
}
