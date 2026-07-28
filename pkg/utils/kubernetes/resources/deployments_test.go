package resources

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestGenerateNewDeploymentForDataPlane(t *testing.T) {
	const (
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
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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

				require.Nil(t, container.SecurityContext)
			},
		},
		{
			name: "with hardening opted in, security context is applied",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "1",
					Namespace: "test-namespace",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							Hardened: commonv1alpha1.HardeningStateEnabled,
						},
					},
				},
			},
			testFunc: func(t *testing.T, deploymentSpec *appsv1.DeploymentSpec) {
				require.Len(t, deploymentSpec.Template.Spec.Containers, 1)
				container := deploymentSpec.Template.Spec.Containers[0]

				require.Equal(t, &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(false),
					ReadOnlyRootFilesystem:   new(true),
					RunAsNonRoot:             new(true),
					RunAsUser:                new(int64(65532)),
					RunAsGroup:               new(int64(65532)),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
						Add:  []corev1.Capability{"NET_BIND_SERVICE"},
					},
				}, container.SecurityContext)
			},
		},
		{
			name: "with CPU resources specified we get merged resources",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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
		{
			name: "with volumes and volume mounts specified",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-name",
					Namespace: "test-namespace",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							Hardened: commonv1alpha1.HardeningStateEnabled,
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Volumes: []corev1.Volume{
											{
												Name: "test-volume",
												VolumeSource: corev1.VolumeSource{
													EmptyDir: &corev1.EmptyDirVolumeSource{},
												},
											},
										},
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
												VolumeMounts: []corev1.VolumeMount{
													{
														Name:      "test-volume",
														MountPath: "/test/path",
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
				expectedVolumes := []corev1.Volume{
					{
						Name: "test-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
					{
						Name: "tmp",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{
								SizeLimit: new(resource.MustParse("1Gi")),
							},
						},
					},
					{
						Name: "var-kong",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				}
				actualVolumes := deploymentSpec.Template.Spec.Volumes
				require.Equal(t, expectedVolumes, actualVolumes)

				expectedVolumesMounts := []corev1.VolumeMount{
					{
						Name:      "test-volume",
						MountPath: "/test/path",
					},
					{
						Name:      "tmp",
						MountPath: "/tmp",
					},
					{
						Name:      "var-kong",
						MountPath: "/var/kong",
					},
				}
				require.Len(t, deploymentSpec.Template.Spec.Containers, 1)
				require.Equal(t, expectedVolumesMounts, deploymentSpec.Template.Spec.Containers[0].VolumeMounts)
			},
		},
		{
			name: "with volumes and volume mounts specified but hardening not opted in, no extra volumes and security context are added",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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
										Volumes: []corev1.Volume{
											{
												Name: "test-volume",
												VolumeSource: corev1.VolumeSource{
													EmptyDir: &corev1.EmptyDirVolumeSource{},
												},
											},
										},
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
												VolumeMounts: []corev1.VolumeMount{
													{
														Name:      "test-volume",
														MountPath: "/test/path",
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
				expectedVolumes := []corev1.Volume{
					{
						Name: "test-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				}
				actualVolumes := deploymentSpec.Template.Spec.Volumes
				require.Equal(t, expectedVolumes, actualVolumes)

				expectedVolumesMounts := []corev1.VolumeMount{
					{
						Name:      "test-volume",
						MountPath: "/test/path",
					},
				}
				require.Len(t, deploymentSpec.Template.Spec.Containers, 1)
				require.Equal(t, expectedVolumesMounts, deploymentSpec.Template.Spec.Containers[0].VolumeMounts)

				require.Nil(t, deploymentSpec.Template.Spec.Containers[0].SecurityContext)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			partial, err := GenerateNewDeploymentForDataPlane(tt.dataplane, dataplaneImage)
			require.NoError(t, err)

			deployment, err := ApplyDeploymentUserPatches(partial, tt.dataplane.Spec.Deployment.PodTemplateSpec)
			require.NoError(t, err)
			tt.testFunc(t, &deployment.Spec)
		})
	}
}

func TestHardenContainerWithSecurityContext(t *testing.T) {
	expectedSecurityContext := &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		ReadOnlyRootFilesystem:   new(true),
		RunAsNonRoot:             new(true),
		RunAsUser:                new(int64(65532)),
		RunAsGroup:               new(int64(65532)),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add:  []corev1.Capability{"NET_BIND_SERVICE"},
		},
	}

	tests := []struct {
		name                 string
		dpType               DataPlaneType
		expectedVolumeMounts []corev1.VolumeMount
		expectedVolumes      []corev1.Volume
		expectedEnvAppended  []corev1.EnvVar
	}{
		{
			name:   "non-KEG container gets tmp and var-kong volumes plus KONG_PREFIX",
			dpType: DataPlaneTypeGateway,
			expectedVolumeMounts: []corev1.VolumeMount{
				{Name: "existing", MountPath: "/existing"},
				{Name: "tmp", MountPath: "/tmp"},
				{Name: "var-kong", MountPath: "/var/kong"},
			},
			expectedVolumes: []corev1.Volume{
				{
					Name: "tmp",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: new(resource.MustParse("1Gi")),
						},
					},
				},
				{
					Name: "var-kong",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			expectedEnvAppended: []corev1.EnvVar{
				{Name: "KONG_PREFIX", Value: "/var/kong"},
			},
		},
		{
			name:   "KEG container only gets tmp volume, no var-kong and no KONG_PREFIX",
			dpType: DataPlaneTypeKeg,
			expectedVolumeMounts: []corev1.VolumeMount{
				{Name: "existing", MountPath: "/existing"},
				{Name: "tmp", MountPath: "/tmp"},
			},
			expectedVolumes: []corev1.Volume{
				{
					Name: "tmp",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: new(resource.MustParse("1Gi")),
						},
					},
				},
			},
			expectedEnvAppended: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := corev1.Container{
				Name: "test-container",
				Env: []corev1.EnvVar{
					{Name: "EXISTING_ENV", Value: "value"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "existing", MountPath: "/existing"},
				},
			}

			container, volumes := HardenContainerWithSecurityContext(input, tt.dpType)

			require.Equal(t, expectedSecurityContext, container.SecurityContext)
			require.Equal(t, tt.expectedVolumeMounts, container.VolumeMounts)
			require.Equal(t, tt.expectedVolumes, volumes)

			expectedEnv := append(
				[]corev1.EnvVar{{Name: "EXISTING_ENV", Value: "value"}},
				tt.expectedEnvAppended...,
			)
			require.Equal(t, expectedEnv, container.Env)
		})
	}
}
