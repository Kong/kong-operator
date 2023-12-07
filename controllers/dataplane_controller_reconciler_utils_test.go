package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/versions"
)

func init() {
	if err := gatewayv1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1 scheme")
		os.Exit(1)
	}
	if err := operatorv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1alpha1 scheme")
		os.Exit(1)
	}
	if err := operatorv1beta1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1beta1 scheme")
		os.Exit(1)
	}
}

func TestEnsureDeploymentForDataPlane(t *testing.T) {
	expectedDeploymentStrategy := appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 0,
			},
			MaxSurge: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 1,
			},
		},
	}

	const developmentMode = false

	testCases := []struct {
		name           string
		dataPlane      *operatorv1beta1.DataPlane
		certSecretName string
		testBody       func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string)
	}{
		{
			name: "no existing DataPlane deployment",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string) {
				ctx := context.Background()
				res, deployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName,
					client.MatchingLabels{
						consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
					},
				)
				require.NoError(t, err)
				require.Equal(t, Created, res)
				require.Equal(t, expectedDeploymentStrategy, deployment.Spec.Strategy)
			},
		},
		{
			name: "new DataPlane with custom secret",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret-volume",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Volumes: []corev1.Volume{
											{
												// NOTE: we need to provide the existing entry in the slice
												// to prevent merging the provided new entry with existing entries.
												Name: consts.ClusterCertificateVolume,
											},
											{
												Name: "test-volume",
												VolumeSource: corev1.VolumeSource{
													Secret: &corev1.SecretVolumeSource{
														SecretName: "test-secret",
													},
												},
											},
										},
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
												VolumeMounts: []corev1.VolumeMount{
													{
														Name:      consts.ClusterCertificateVolume,
														MountPath: consts.ClusterCertificateVolumeMountPath,
													},
													{
														Name:      "test-volume",
														MountPath: "/var/test/",
														ReadOnly:  true,
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
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string) {
				ctx := context.Background()

				res, deployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName,
					client.MatchingLabels{
						consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
					},
				)
				require.NoError(t, err)
				require.Equal(t, Created, res)
				require.Len(t, deployment.Spec.Template.Spec.Volumes, 2)
				require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
				require.Len(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts, 2)

				certificateVolume := corev1.Volume{}
				certificateVolume.Secret = &corev1.SecretVolumeSource{}
				// Fill in the defaults for the volume after setting the secret volume source
				// field. This prevents setting the empty dir volume source field which
				// would conflict with the secret volume source field.
				k8sresources.SetDefaultsVolume(&certificateVolume)
				certificateVolume.Name = consts.ClusterCertificateVolume
				certificateVolume.VolumeSource.Secret.SecretName = "certificate"
				certificateVolume.VolumeSource.Secret.Items = []corev1.KeyToPath{
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
				}

				testVolume := corev1.Volume{}
				testVolume.Secret = &corev1.SecretVolumeSource{}
				// Fill in the defaults for the volume after setting the secret volume source
				// field. This prevents setting the empty dir volume source field which
				// would conflict with the secret volume source field.
				k8sresources.SetDefaultsVolume(&testVolume)
				testVolume.Name = "test-volume"
				testVolume.VolumeSource.Secret.SecretName = "test-secret"
				require.Equal(t,
					[]corev1.Volume{certificateVolume, testVolume},
					deployment.Spec.Template.Spec.Volumes,
				)

				require.Equal(t, []corev1.VolumeMount{
					{
						Name:      consts.ClusterCertificateVolume,
						MountPath: consts.ClusterCertificateVolumeMountPath,
						ReadOnly:  true,
					},
					{
						Name:      "test-volume",
						MountPath: "/var/test/",
						ReadOnly:  true,
					},
				},
					deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
				)
			},
		},
		{
			name: "existing DataPlane deployment gets updated with expected spec.Strategy",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string) {
				ctx := context.Background()
				dataplaneImage, err := generateDataPlaneImage(dataPlane, versions.IsDataPlaneImageVersionSupported)
				require.NoError(t, err)
				// generate the DataPlane as it is supposed to be, change the .spec.strategy field, and create it.
				existingDeployment, err := k8sresources.GenerateNewDeploymentForDataPlane(dataPlane, dataplaneImage, certSecretName)
				require.NoError(t, err)
				existingDeployment.Spec.Strategy.RollingUpdate.MaxUnavailable = &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 5,
				}
				require.NoError(t, reconciler.Client.Create(ctx, existingDeployment))

				res, deployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName, client.MatchingLabels{})
				require.NoError(t, err)

				assert.Equal(t, Updated, res, "the DataPlane deployment should be updated with the original strategy")
				assert.Equal(t, expectedDeploymentStrategy, deployment.Spec.Strategy)
			},
		},
		{
			name: "existing DataPlane deployment does get updated when it doesn't have the resources equal to defaults",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
												Resources: corev1.ResourceRequirements{
													Requests: corev1.ResourceList{
														corev1.ResourceCPU:    resource.MustParse("2"),
														corev1.ResourceMemory: resource.MustParse("1237Mi"),
													},
													Limits: corev1.ResourceList{
														corev1.ResourceCPU:    resource.MustParse("3"),
														corev1.ResourceMemory: resource.MustParse("1237Mi"),
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
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string) {
				ctx := context.Background()
				dataplaneImage, err := generateDataPlaneImage(dataPlane, versions.IsDataPlaneImageVersionSupported)
				require.NoError(t, err)
				// generate the DataPlane as it is expected to be and create it.
				existingDeployment, err := k8sresources.GenerateNewDeploymentForDataPlane(dataPlane, dataplaneImage, certSecretName)
				require.NoError(t, err)

				// generateDataPlaneImage will set deployment's containers resources
				// to the ones set in dataplane spec so we set it here to get the
				// expected behavior in reconciler's ensureDeploymentForDataPlane().reconciler.Client,
				dataPlane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("4")

				require.NoError(t, reconciler.Client.Create(ctx, existingDeployment))

				res, deployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName, client.MatchingLabels{})
				require.NoError(t, err)

				assert.Equal(t, Updated, res, "the DataPlane deployment should be updated to get the resources set to defaults")
				require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
				require.Equal(t, dataPlane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Resources, deployment.Spec.Template.Spec.Containers[0].Resources)
			},
		},
		{
			name: "existing DataPlane deployment does get updated when it doesn't have the affinity set",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Affinity: &corev1.Affinity{
											PodAntiAffinity: &corev1.PodAntiAffinity{
												PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
													{
														PodAffinityTerm: corev1.PodAffinityTerm{
															TopologyKey: "kubernetes.io/hostname",
															LabelSelector: &metav1.LabelSelector{
																MatchLabels: map[string]string{
																	"workload-type": "dataplane",
																},
															},
															NamespaceSelector: &metav1.LabelSelector{},
														},
													},
												},
											},
										},
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string) {
				ctx := context.Background()
				dataplaneImage, err := generateDataPlaneImage(dataPlane, versions.IsDataPlaneImageVersionSupported)
				// generateDataPlaneImage will set deployment's containers resources
				// to the ones set in dataplane spec so we set it here to get the
				// expected behavior in reconciler's ensureDeploymentForDataPlane()
				require.NoError(t, err)
				// generate the DataPlane as it is expected to be and create it.
				existingDeployment, err := k8sresources.GenerateNewDeploymentForDataPlane(dataPlane, dataplaneImage, certSecretName)
				require.NoError(t, err)

				dataPlane.Spec.Deployment.PodTemplateSpec.Spec.Affinity = &corev1.Affinity{}

				require.NoError(t, reconciler.Client.Create(ctx, existingDeployment))

				res, deployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName, client.MatchingLabels{})
				require.NoError(t, err)

				assert.Equal(t, Updated, res, "the DataPlane deployment should be updated to get the affinity set to the dataplane's spec")
				assert.Len(t, deployment.Spec.Template.Spec.Containers, 1)
				assert.Equal(t, dataPlane.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec.Affinity.PodAntiAffinity, deployment.Spec.Template.Spec.Affinity.PodAntiAffinity)
			},
		},
		{
			name: "existing DataPlane deployment does get updated when affinity is unset in the spec but set in the deployment",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Affinity: &corev1.Affinity{},
									},
								},
							},
						},
					},
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string) {
				ctx := context.Background()

				res, existingDeployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName,
					client.MatchingLabels{
						consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
					},
				)
				require.NoError(t, err)
				require.Equal(t, Created, res)

				existingDeployment.Spec.Template.Spec.Affinity = &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"workload-type": "dataplane",
										},
									},
									NamespaceSelector: &metav1.LabelSelector{},
								},
							},
						},
					},
				}

				require.NoError(t, reconciler.Client.Update(ctx, existingDeployment))

				res, deployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName,
					client.MatchingLabels{
						consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
					},
				)
				require.NoError(t, err)
				assert.Equal(t, Updated, res, "the DataPlane deployment should be updated to get the affinity removed")
				require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
				require.Equal(t, deployment.Spec.Template.Spec.Affinity, &corev1.Affinity{})
			},
		},
		{
			name: "DataPlane deployment does get created with specified volumes and volume mounts",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Affinity: &corev1.Affinity{},
									},
								},
							},
						},
					},
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1beta1.DataPlane, certSecretName string) {
				ctx := context.Background()

				res, existingDeployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName,
					client.MatchingLabels{
						consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
					},
				)
				require.NoError(t, err)
				require.Equal(t, Created, res)

				existingDeployment.Spec.Template.Spec.Affinity = &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"workload-type": "dataplane",
										},
									},
									NamespaceSelector: &metav1.LabelSelector{},
								},
							},
						},
					},
				}

				require.NoError(t, reconciler.Client.Update(ctx, existingDeployment))

				res, deployment, err := ensureDeploymentForDataPlane(ctx, reconciler.Client, logr.Discard(), developmentMode, dataPlane, certSecretName,
					client.MatchingLabels{
						consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
					},
				)
				require.NoError(t, err)
				assert.Equal(t, Updated, res, "the DataPlane deployment should be updated to get the affinity removed")
				require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
				require.Equal(t, deployment.Spec.Template.Spec.Affinity, &corev1.Affinity{})
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithObjects(tc.dataPlane).
			WithScheme(scheme.Scheme).
			Build()

		reconciler := DataPlaneReconciler{
			Client: fakeClient,
		}

		t.Run(tc.name, func(t *testing.T) {
			tc.testBody(t, reconciler, tc.dataPlane, tc.certSecretName)
		})
	}
}

func TestDataPlaneIngressServiceIsReady(t *testing.T) {
	withLoadBalancerIngressStatus := func(lb corev1.LoadBalancerIngress) func(*corev1.Service) {
		return func(s *corev1.Service) {
			s.Status.LoadBalancer.Ingress = append(s.Status.LoadBalancer.Ingress, lb)
		}
	}

	ingressService := func(opts ...func(*corev1.Service)) *corev1.Service {
		s := &corev1.Service{}
		for _, opt := range opts {
			opt(s)
		}
		return s
	}

	testCases := []struct {
		name                    string
		dataPlane               *operatorv1beta1.DataPlane
		dataPlaneIngressService *corev1.Service
		expected                bool
	}{
		{
			name:                    "returns true when DataPlane not have a Load Balancer Ingress Service set",
			dataPlane:               NewTestDataPlaneBuilder().WithIngressServiceType(corev1.ServiceTypeClusterIP).Build(),
			dataPlaneIngressService: ingressService(),
			expected:                true,
		},
		{
			name:      "returns true when DataPlane has a Load Balancer Ingress Service set with an IP",
			dataPlane: NewTestDataPlaneBuilder().WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			dataPlaneIngressService: ingressService(
				withLoadBalancerIngressStatus(corev1.LoadBalancerIngress{
					IP: "10.0.0.1",
				}),
			),
			expected: true,
		},
		{
			name:      "returns true when DataPlane has a Load Balancer Ingress Service set with a Hostname",
			dataPlane: NewTestDataPlaneBuilder().WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			dataPlaneIngressService: ingressService(
				withLoadBalancerIngressStatus(corev1.LoadBalancerIngress{
					Hostname: "random-hostname.example.com",
				}),
			),
			expected: true,
		},
		{
			name:      "returns true when DataPlane has a Load Balancer Ingress Service set with an IP and Hostname",
			dataPlane: NewTestDataPlaneBuilder().WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			dataPlaneIngressService: ingressService(
				withLoadBalancerIngressStatus(corev1.LoadBalancerIngress{
					IP:       "10.0.0.1",
					Hostname: "random-hostname.example.com",
				}),
			),
			expected: true,
		},
		{
			name:                    "returns false when DataPlane has a Load Balancer Ingress Service set without an IP or Hostname",
			dataPlane:               NewTestDataPlaneBuilder().WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			dataPlaneIngressService: ingressService(),
			expected:                false,
		},
		{
			name:      "returns true when DataPlane has a Load Balancer Ingress Service set with 2 status and only the second one having an IP",
			dataPlane: NewTestDataPlaneBuilder().WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			dataPlaneIngressService: ingressService(
				withLoadBalancerIngressStatus(corev1.LoadBalancerIngress{}), // Shouldn't really happen though
				withLoadBalancerIngressStatus(corev1.LoadBalancerIngress{
					IP: "10.0.0.1",
				}),
			),
			expected: true,
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			res := dataPlaneIngressServiceIsReady(tc.dataPlane, tc.dataPlaneIngressService)
			assert.Equal(t, tc.expected, res)
		})
	}
}
