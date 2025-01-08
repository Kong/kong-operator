package resources

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
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

func TestGenerateNewDeploymentForControlPlane(t *testing.T) {
	tests := []struct {
		name                     string
		generateControlPlaneArgs GenerateNewDeploymentForControlPlaneParams
		expectedDeployment       *appsv1.Deployment
	}{
		{
			name: "base case works",
			generateControlPlaneArgs: GenerateNewDeploymentForControlPlaneParams{
				ControlPlane: &operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "test-namespace",
						UID:       types.UID("1234-5678-9012"),
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: "gateway-operator.konghq.com/v1beta1",
						Kind:       "ControlPlane",
					},
				},
				ControlPlaneImage:              "kong/kubernetes-ingress-controller:3.1.5",
				AdmissionWebhookCertSecretName: "admission-webhook-certificate",
				AdminMTLSCertSecretName:        "cluster-certificate-secret-name",
			},
			expectedDeployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "controlplane-cp-1-",
					Namespace:    "test-namespace",
					Labels: map[string]string{
						"app":                                    "cp-1",
						"gateway-operator.konghq.com/managed-by": "controlplane",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway-operator.konghq.com/v1beta1",
							Kind:       "ControlPlane",
							Name:       "cp-1",
							UID:        types.UID("1234-5678-9012"),
							Controller: lo.ToPtr(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: lo.ToPtr(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "cp-1",
						},
					},
					RevisionHistoryLimit:    lo.ToPtr(int32(10)),
					ProgressDeadlineSeconds: lo.ToPtr(int32(600)),
					Strategy: appsv1.DeploymentStrategy{
						Type: appsv1.DeploymentStrategyType("RollingUpdate"),
						RollingUpdate: &appsv1.RollingUpdateDeployment{
							MaxUnavailable: lo.ToPtr(intstr.FromString("25%")),
							MaxSurge:       lo.ToPtr(intstr.FromString("25%")),
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "cp-1",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "cluster-certificate",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  "cluster-certificate-secret-name",
											DefaultMode: lo.ToPtr(int32(420)),
											Items: []corev1.KeyToPath{
												{
													Key:  "tls.crt",
													Path: "tls.crt",
												},
												{
													Key:  "tls.key",
													Path: "tls.key",
												},
												{
													Key:  "ca.crt",
													Path: "ca.crt",
												},
											},
										},
									},
								},
								{
									Name: "admission-webhook-certificate",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  "admission-webhook-certificate",
											DefaultMode: lo.ToPtr(int32(420)),
											Items: []corev1.KeyToPath{
												{
													Key:  "tls.crt",
													Path: "tls.crt",
												},
												{
													Key:  "tls.key",
													Path: "tls.key",
												},
											},
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: "kong/kubernetes-ingress-controller:3.1.5",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("20Mi"),
											corev1.ResourceCPU:    resource.MustParse("100m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("100Mi"),
											corev1.ResourceCPU:    resource.MustParse("200m"),
										},
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "health",
											ContainerPort: 10254,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "webhook",
											ContainerPort: 8080,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "cluster-certificate",
											MountPath: "/var/cluster-certificate",
											ReadOnly:  true,
										},
										{
											Name:      "admission-webhook-certificate",
											MountPath: "/admission-webhook",
											ReadOnly:  true,
										},
									},
									LivenessProbe:            GenerateControlPlaneProbe("/healthz", intstr.FromInt(10254)),
									ReadinessProbe:           GenerateControlPlaneProbe("/readyz", intstr.FromInt(10254)),
									TerminationMessagePath:   "/dev/termination-log",
									TerminationMessagePolicy: corev1.TerminationMessageReadFile,
									ImagePullPolicy:          corev1.PullIfNotPresent,
								},
							},
							SecurityContext:               &corev1.PodSecurityContext{},
							RestartPolicy:                 corev1.RestartPolicyAlways,
							DNSPolicy:                     corev1.DNSClusterFirst,
							SchedulerName:                 corev1.DefaultSchedulerName,
							TerminationGracePeriodSeconds: lo.ToPtr(int64(30)),
						},
					},
				},
			},
		},
		{
			name: "no webhook cert secret name specified doesn't the webhook volume, volume mount nor port",
			generateControlPlaneArgs: GenerateNewDeploymentForControlPlaneParams{
				ControlPlane: &operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "test-namespace",
						UID:       types.UID("1234-5678-9012"),
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: "gateway-operator.konghq.com/v1beta1",
						Kind:       "ControlPlane",
					},
				},
				ControlPlaneImage:       "kong/kubernetes-ingress-controller:3.1.5",
				AdminMTLSCertSecretName: "cluster-certificate-secret-name",
			},
			expectedDeployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "controlplane-cp-1-",
					Namespace:    "test-namespace",
					Labels: map[string]string{
						"app":                                    "cp-1",
						"gateway-operator.konghq.com/managed-by": "controlplane",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway-operator.konghq.com/v1beta1",
							Kind:       "ControlPlane",
							Name:       "cp-1",
							UID:        types.UID("1234-5678-9012"),
							Controller: lo.ToPtr(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: lo.ToPtr(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "cp-1",
						},
					},
					RevisionHistoryLimit:    lo.ToPtr(int32(10)),
					ProgressDeadlineSeconds: lo.ToPtr(int32(600)),
					Strategy: appsv1.DeploymentStrategy{
						Type: appsv1.DeploymentStrategyType("RollingUpdate"),
						RollingUpdate: &appsv1.RollingUpdateDeployment{
							MaxUnavailable: lo.ToPtr(intstr.FromString("25%")),
							MaxSurge:       lo.ToPtr(intstr.FromString("25%")),
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "cp-1",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "cluster-certificate",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  "cluster-certificate-secret-name",
											DefaultMode: lo.ToPtr(int32(420)),
											Items: []corev1.KeyToPath{
												{
													Key:  "tls.crt",
													Path: "tls.crt",
												},
												{
													Key:  "tls.key",
													Path: "tls.key",
												},
												{
													Key:  "ca.crt",
													Path: "ca.crt",
												},
											},
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: "kong/kubernetes-ingress-controller:3.1.5",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("20Mi"),
											corev1.ResourceCPU:    resource.MustParse("100m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("100Mi"),
											corev1.ResourceCPU:    resource.MustParse("200m"),
										},
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "health",
											ContainerPort: 10254,
											Protocol:      corev1.ProtocolTCP,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "cluster-certificate",
											MountPath: "/var/cluster-certificate",
											ReadOnly:  true,
										},
									},
									LivenessProbe:            GenerateControlPlaneProbe("/healthz", intstr.FromInt(10254)),
									ReadinessProbe:           GenerateControlPlaneProbe("/readyz", intstr.FromInt(10254)),
									TerminationMessagePath:   "/dev/termination-log",
									TerminationMessagePolicy: corev1.TerminationMessageReadFile,
									ImagePullPolicy:          corev1.PullIfNotPresent,
								},
							},
							SecurityContext:               &corev1.PodSecurityContext{},
							RestartPolicy:                 corev1.RestartPolicyAlways,
							DNSPolicy:                     corev1.DNSClusterFirst,
							SchedulerName:                 corev1.DefaultSchedulerName,
							TerminationGracePeriodSeconds: lo.ToPtr(int64(30)),
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment, err := GenerateNewDeploymentForControlPlane(tt.generateControlPlaneArgs)
			require.NoError(t, err)
			require.Equal(t, tt.expectedDeployment, deployment)
		})
	}
}
