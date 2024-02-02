package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestDataPlaneEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"label-a": "value-a",
									"label-x": "value-x",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Env: []corev1.EnvVar{
											{
												Name:  "TEST_ENV",
												Value: "test",
											},
										},
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
									},
								},
							},
						},
					},
				},
				Network: operatorv1beta1.DataPlaneNetworkOptions{
					Services: &operatorv1beta1.DataPlaneServices{
						Ingress: &operatorv1beta1.ServiceOptions{
							Annotations: map[string]string{
								"foo": "bar",
							},
						},
					},
				},
			},
		},
	}

	dataplaneClient := GetClients().OperatorClient.ApisV1beta1().DataPlanes(namespace.Name)
	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), time.Minute, time.Second)

	t.Logf("verifying that pod labels were set per the provided spec")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
		deployment := &deployments[0]

		va, oka := deployment.Spec.Template.Labels["label-a"]
		if !oka || va != "value-a" {
			t.Logf("got unexpected %q label-a value", va)
			return false
		}
		vx, okx := deployment.Spec.Template.Labels["label-x"]
		if !okx || vx != "value-x" {
			t.Logf("got unexpected %q label-x value", vx)
			return false
		}

		return true
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	// check environment variables of deployments and pods.

	t.Log("verifying dataplane Deployment.Pods.Env vars")
	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)
	envs := proxyContainer.Env
	// check specified custom envs
	testEnvValue := getEnvValueByName(envs, "TEST_ENV")
	require.Equal(t, "test", testEnvValue)
	// check default envs added by operator
	kongDatabaseEnvValue := getEnvValueByName(envs, consts.EnvVarKongDatabase)
	require.Equal(t, "off", kongDatabaseEnvValue)

	t.Log("verifying services managed by the dataplane")
	var dataplaneIngressService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneIngressService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), time.Minute, time.Second)
	t.Log("verifying annotations on the proxy service managed by the dataplane")
	require.Equal(t, dataplaneIngressService.Annotations["foo"], "bar")

	t.Log("verifying dataplane services receive IP addresses")
	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), dataplaneIngressService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, time.Minute, time.Second)

	require.Eventually(t, expect404WithNoRouteFunc(t, GetCtx(), "http://"+dataplaneIP), time.Minute, time.Second)

	t.Log("deleting the dataplane deployment")
	dataplaneDeployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, dataplaneDeployments, 1, "there must be only one dataplane deployment")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), &dataplaneDeployments[0]))

	t.Log("verifying deployments managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), time.Minute, time.Second)

	t.Log("deleting the dataplane service")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), &dataplaneIngressService))

	t.Log("verifying services managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneIngressService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), time.Minute, time.Second)

	t.Log("verifying dataplane services receive IP addresses after deletion")
	require.Eventually(t, func() bool {
		dataplaneService, err := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), dataplaneIngressService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, time.Minute, time.Second)

	require.Eventually(t, expect404WithNoRouteFunc(t, GetCtx(), "http://"+dataplaneIP), time.Minute, time.Second)

	t.Log("verifying dataplane status is properly filled with backing service name and its addresses")
	require.Eventually(t, testutils.DataPlaneHasServiceAndAddressesInStatus(t, GetCtx(), dataplaneName, clients), time.Minute, time.Second)

	t.Log("updating dataplane spec with proxy service type of ClusterIP")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Network.Services.Ingress.Type = corev1.ServiceTypeClusterIP
		}),
		time.Minute, time.Second)

	t.Log("checking if dataplane proxy service type changes to ClusterIP")
	require.Eventually(t, func() bool {
		servicesClient := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace)
		dataplaneIngressService, err := servicesClient.Get(GetCtx(), dataplaneIngressService.Name, metav1.GetOptions{})
		if err != nil {
			t.Logf("error getting dataplane proxy service: %v", err)
			return false
		}
		if dataplaneIngressService.Spec.Type != corev1.ServiceTypeClusterIP {
			t.Logf("dataplane proxy service should be of ClusterIP type but is %s", dataplaneIngressService.Spec.Type)
			return false
		}

		return true
	}, time.Minute, time.Second)
}

func TestDataPlaneUpdate(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	dataplaneClient := GetClients().OperatorClient.ApisV1beta1().DataPlanes(namespace.Name)
	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Env: []corev1.EnvVar{
											{
												Name:  "TEST_ENV",
												Value: "before_update",
											},
											{
												Name:  consts.EnvVarKongDatabase,
												Value: "off",
											},
										},
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying that the dataplane gets marked as provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(
		t, GetCtx(), dataplaneName, GetClients().OperatorClient),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t,
		testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		}, clients),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService corev1.Service
	require.Eventually(t,
		testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneService, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
		}),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DatePlane deployment")
	deployment := &deployments[0]

	t.Logf("verifying environment variable TEST_ENV in deployment before update")
	container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)
	testEnv := getEnvValueByName(container.Env, "TEST_ENV")
	require.Equal(t, "before_update", testEnv)

	t.Logf("updating dataplane resource")
	dataplane, err = dataplaneClient.Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
	require.NoError(t, err)
	container = k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)
	container.Env = []corev1.EnvVar{
		{
			Name: "TEST_ENV", Value: "after_update",
		},
	}
	dataplane, err = dataplaneClient.Update(GetCtx(), dataplane, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Logf("verifying environment variable TEST_ENV in deployment after update")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
		deployment := &deployments[0]

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		testEnv := getEnvValueByName(container.Env, "TEST_ENV")
		t.Logf("Tenvironment variable TEST_ENV is now %s in deployment", testEnv)
		return testEnv == "after_update"
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	dataPlaneConditionPredicate := func(t *testing.T, c *metav1.Condition) func(dataplane *operatorv1beta1.DataPlane) bool {
		t.Helper()
		return func(dataplane *operatorv1beta1.DataPlane) bool {
			t.Helper()
			for _, condition := range dataplane.Status.Conditions {
				if condition.Type == c.Type && condition.Status == c.Status && condition.Reason == c.Reason && condition.ObservedGeneration == c.ObservedGeneration {
					return true
				}
				t.Logf("DataPlane %q condition: Type=%q;ObservedGeneration:%d,Reason:%q;Status:%q;Message:%q",
					dataplane.Name, condition.Type, condition.ObservedGeneration, condition.Reason, condition.Status, condition.Message,
				)
			}
			return false
		}
	}

	t.Run("dataplane is not Ready when the underlying deployment changes state to not Ready", func(t *testing.T) {
		dataplane, err = dataplaneClient.Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)
		container := k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.DeploymentOptions.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		container.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 0,
			PeriodSeconds:       1,
			FailureThreshold:    3,
			SuccessThreshold:    1,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/status_which_will_always_return_404",
					Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		}
		dataplane, err = dataplaneClient.Update(GetCtx(), dataplane, metav1.UpdateOptions{})
		require.NoError(t, err)

		isNotReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(k8sutils.ReadyType),
			Status:             metav1.ConditionFalse,
			Reason:             string(k8sutils.WaitingToBecomeReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isNotReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})
	t.Run("dataplane gets Ready when the underlying deployment changes state to Ready", func(t *testing.T) {
		dataplane, err = dataplaneClient.Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)
		container := k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.DeploymentOptions.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		container.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 0,
			PeriodSeconds:       1,
			FailureThreshold:    3,
			SuccessThreshold:    1,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/status",
					Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		}
		dataplane, err = dataplaneClient.Update(GetCtx(), dataplane, metav1.UpdateOptions{})
		require.NoError(t, err)

		isReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(k8sutils.ReadyType),
			Status:             metav1.ConditionTrue,
			Reason:             string(k8sutils.ResourceReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})

	t.Run("dataplane Ready condition gets properly update with correct ObservedGeneration", func(t *testing.T) {
		dataplane, err = dataplaneClient.Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)
		container := k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.DeploymentOptions.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		container.StartupProbe = &corev1.Probe{
			InitialDelaySeconds: 0,
			PeriodSeconds:       2,
			FailureThreshold:    30,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/status",
					Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		}

		dataplane, err = dataplaneClient.Update(GetCtx(), dataplane, metav1.UpdateOptions{})
		require.NoError(t, err)

		isReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(k8sutils.ReadyType),
			Status:             metav1.ConditionTrue,
			Reason:             string(k8sutils.ResourceReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})

	t.Run("dataplane gets properly updated with a ReadinessProbe using port names instead of numbers", func(t *testing.T) {
		dataplane, err = dataplaneClient.Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)
		container := k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.DeploymentOptions.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		container.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 0,
			PeriodSeconds:       2,
			FailureThreshold:    30,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/status",
					Port:   intstr.FromString("metrics"),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		}

		dataplane, err = dataplaneClient.Update(GetCtx(), dataplane, metav1.UpdateOptions{})
		require.NoError(t, err)

		isReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(k8sutils.ReadyType),
			Status:             metav1.ConditionTrue,
			Reason:             string(k8sutils.ResourceReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})
}

func TestDataPlaneHorizontalScaling(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying dataplane resource with 2 replicas")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(2)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	dataplaneClient := GetClients().OperatorClient.ApisV1beta1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	deployment := &appsv1.Deployment{}
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, deployment, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), time.Minute, time.Second)

	t.Log("verifying that dataplane has indeed 2 ready replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 2), time.Minute, time.Second)

	t.Log("changing replicas in dataplane spec to 1 should scale down the deployment back to 1")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) { dp.Spec.Deployment.Replicas = lo.ToPtr(int32(1)) }),
		time.Minute, time.Second)

	t.Log("verifying that dataplane has indeed 1 ready replica after scaling down")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 1), time.Minute, time.Second)

	t.Log("changing from replicas to using autoscaling should create an HPA targeting the dataplane deployment")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.Scaling = &operatorv1beta1.Scaling{
				HorizontalScaling: &operatorv1beta1.HorizontalScaling{
					MaxReplicas: 3,
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricSource{
								Name: "cpu",
								Target: autoscalingv2.MetricTarget{
									Type:               autoscalingv2.UtilizationMetricType,
									AverageUtilization: lo.ToPtr(int32(20)),
								},
							},
						},
					},
				},
			}
			dp.Spec.Deployment.Replicas = nil
		}),
		time.Minute, time.Second)

	{
		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.Eventually(t, testutils.DataPlaneHasHPA(t, GetCtx(), dataplane, &hpa, clients), time.Minute, time.Second)
		require.NotNil(t, hpa)
		assert.Equal(t, int32(3), hpa.Spec.MaxReplicas)
		require.Len(t, hpa.Spec.Metrics, 1)
		require.NotNil(t, hpa.Spec.Metrics[0].Resource)
		require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
		assert.Equal(t, int32(20), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	}

	t.Log("updating the horizontal scaling spec should update the relevant HPA")
	require.Eventuallyf(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.Scaling.HorizontalScaling.MaxReplicas = 5
			dp.Spec.Deployment.Scaling.HorizontalScaling.Metrics[0].Resource.Target.AverageUtilization = lo.ToPtr(int32(50))
			dp.Spec.Deployment.Replicas = nil
		}),
		time.Minute, time.Second, "failed to update dataplane %s", dataplane.Name)
	require.Eventuallyf(t, func() bool {
		hpas := testutils.MustListDataPlaneHPAs(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		if len(hpas) != 1 {
			return false
		}

		hpa := hpas[0]
		if hpa.Spec.MaxReplicas != 5 {
			return false
		}
		if len(hpa.Spec.Metrics) != 1 || hpa.Spec.Metrics[0].Resource == nil ||
			hpa.Spec.Metrics[0].Resource.Target.AverageUtilization == nil ||
			*hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != 50 {
			return false
		}
		return true
	}, time.Minute, time.Second, "HPA for dataplane %s not found or not as expected", dataplane.Name)

	t.Log("removing the horizontal scaling spec should delete the relevant HPA")
	require.Eventuallyf(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.Scaling = nil
		}),
		time.Minute, time.Second, "failed to update dataplane %s", dataplane.Name)
	require.Eventuallyf(t, func() bool {
		hpas := testutils.MustListDataPlaneHPAs(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		return len(hpas) == 0
	}, time.Minute, time.Second, "HPA for dataplane %s found but should be deleted", dataplane.Name)
}

func TestDataPlaneVolumeMounts(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("creating a secret to mount to dataplane containers")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		StringData: map[string]string{
			"file-0": "foo",
		},
	}
	secret, err := GetClients().K8sClient.CoreV1().Secrets(namespace.Name).Create(GetCtx(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(secret)

	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Volumes: []corev1.Volume{
									{
										Name: consts.ClusterCertificateVolume,
									},
									{
										Name: "test-volume",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: secret.Name,
											},
										},
									},
								},
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
										VolumeMounts: []corev1.VolumeMount{
											{
												Name:      consts.ClusterCertificateVolume,
												MountPath: consts.ClusterCertificateVolumeMountPath,
												ReadOnly:  true,
											},
											{
												Name:      "test-volume",
												MountPath: "/var/test",
												ReadOnly:  true,
											},
										},
										// Make the test a bit faster.
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 0,
											PeriodSeconds:       1,
											FailureThreshold:    3,
											SuccessThreshold:    1,
											TimeoutSeconds:      1,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path:   "/status",
													Port:   intstr.FromInt32(consts.DataPlaneMetricsPort),
													Scheme: corev1.URISchemeHTTP,
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
	}
	dataplane, err = GetClients().OperatorClient.ApisV1beta1().DataPlanes(namespace.Name).Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying that the dataplane gets marked as Ready")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), time.Minute, time.Second)

	t.Log("verifying dataplane deployment volume mounts")
	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)

	t.Log("verifying dataplane has the default cluster-certificate volume")
	defaultVol := getVolumeByName(deployment.Spec.Template.Spec.Volumes, consts.ClusterCertificateVolume)
	require.NotNil(t, defaultVol, "dataplane pod should have the cluster-certificate volume")

	t.Log("verifying dataplane has the custom test-volume volume")
	vol := getVolumeByName(deployment.Spec.Template.Spec.Volumes, "test-volume")
	require.NotNil(t, vol, "dataplane pod should have the test-volume volume")
	require.NotNil(t, vol.Secret, "test-volume volume should come from secret")

	t.Log("verifying Kong proxy container has mounted the default cluster-certificate volume")
	defVolumeMounts := getVolumeMountsByVolumeName(proxyContainer.VolumeMounts, consts.ClusterCertificateVolume)
	require.Len(t, defVolumeMounts, 1, "proxy container should mount cluster-certificate volume")
	require.Equal(t, defVolumeMounts[0].MountPath, consts.ClusterCertificateVolumeMountPath, "proxy container should mount cluster-certificate volume to path /var/cluster-certificate")
	require.True(t, defVolumeMounts[0].ReadOnly, "proxy container should mount cluster-certificate volume in read only mode")

	t.Log("verifying Kong proxy container has mounted the custom test-volume volume")
	volMounts := getVolumeMountsByVolumeName(proxyContainer.VolumeMounts, "test-volume")
	require.Len(t, volMounts, 1, "proxy container should mount test-volume volume")
	require.Equal(t, volMounts[0].MountPath, "/var/test", "proxy container should mount test-volume volume to path /var/test")
	require.True(t, volMounts[0].ReadOnly, "proxy container should mount test-volume volume in read only mode")

	t.Log("verifying dataplane pod has custom secret volume")
	vol = getVolumeByName(deployment.Spec.Template.Spec.Volumes, "test-volume")
	require.NotNil(t, vol, "dataplane pod should have the volume test-volume")
	require.NotNil(t, vol.Secret, "test-volume should come from secret")
	require.Equalf(t, vol.Secret.SecretName, secret.Name, "test-volume should come from secret %s", secret.Name)

	t.Log("verifying Kong proxy container has mounted custom secret volume")
	volMounts = getVolumeMountsByVolumeName(proxyContainer.VolumeMounts, "test-volume")
	require.Len(t, volMounts, 1, "proxy container should mount custom secret volume 'test-volume'")
	require.Equal(t, volMounts[0].MountPath, "/var/test", "proxy container should mount custom secret volume to path /var/test")
	require.True(t, volMounts[0].ReadOnly, "proxy container should mount 'test-volume' in read only mode")

	t.Log("updating volumes and volume mounts in dataplane spec to verify volumes could be reconciled")
	dataplane.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec.Volumes[1] = corev1.Volume{
		Name: "test-volume-1",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secret.Name,
			},
		},
	}
	dataplane.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec.Containers[0].VolumeMounts[1] = corev1.VolumeMount{
		Name:      "test-volume-1",
		MountPath: "/var/test",
		ReadOnly:  true,
	}
	_, err = GetClients().K8sClient.AppsV1().Deployments(namespace.Name).Update(GetCtx(), deployment, metav1.UpdateOptions{})
	require.NoError(t, err, "should update deployment successfully")

	require.Eventually(t, func() bool {
		deployment, err = GetClients().K8sClient.AppsV1().Deployments(namespace.Name).Get(GetCtx(), deployment.Name, metav1.GetOptions{})
		require.NoError(t, err, "should get dataplane deployment successfully")
		vol := getVolumeByName(deployment.Spec.Template.Spec.Volumes, "test-volume")
		if vol == nil || vol.Secret == nil || vol.Secret.SecretName != secret.Name {
			return false
		}
		proxyContainer := k8sutils.GetPodContainerByName(
			&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNilf(t, proxyContainer, "dataplane deployment should have container %s", consts.DataPlaneProxyContainerName)
		volMounts = getVolumeMountsByVolumeName(proxyContainer.VolumeMounts, "test-volume")
		if len(volMounts) != 1 {
			return false
		}
		if volMounts[0].MountPath != "/var/test" || !volMounts[0].ReadOnly {
			return false
		}
		return true
	}, time.Minute, time.Second)
}
