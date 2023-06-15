//go:build integration_tests
// +build integration_tests

package integration

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestDataplaneEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: v1alpha1.DataPlaneSpec{
			DataPlaneOptions: v1alpha1.DataPlaneOptions{
				Deployment: v1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Labels: map[string]string{
							"label-a": "value-a",
							"label-x": "value-x",
						},
						Env: []corev1.EnvVar{
							{Name: "TEST_ENV", Value: "test"},
						},
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
				Services: operatorv1alpha1.DataPlaneServicesOptions{
					Proxy: &operatorv1alpha1.ProxyServiceOptions{
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
				},
			},
		},
	}

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsProvisioned(t, ctx, dataplaneName, clients.OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients), time.Minute, time.Second)

	t.Logf("verifying that pod labels were set per the provided spec")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
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
	deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
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
	var dataplaneProxyService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneProxyService, clients), time.Minute, time.Second)
	t.Log("verifying annotations on the proxy service managed by the dataplane")
	require.Equal(t, dataplaneProxyService.Annotations["foo"], "bar")

	t.Log("verifying dataplane services receive IP addresses")
	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := clients.K8sClient.CoreV1().Services(dataplane.Namespace).Get(ctx, dataplaneProxyService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, time.Minute, time.Second)

	verifyConnectivity(t, dataplaneIP)

	t.Log("deleting the dataplane deployment")
	dataplaneDeployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
	require.Len(t, dataplaneDeployments, 1, "there must be only one dataplane deployment")
	require.NoError(t, clients.MgrClient.Delete(ctx, &dataplaneDeployments[0]))

	t.Log("verifying deployments managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients), time.Minute, time.Second)

	t.Log("deleting the dataplane service")
	require.NoError(t, clients.MgrClient.Delete(ctx, &dataplaneProxyService))

	t.Log("verifying services managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneProxyService, clients), time.Minute, time.Second)

	t.Log("verifying dataplane services receive IP addresses after deletion")
	require.Eventually(t, func() bool {
		dataplaneService, err := clients.K8sClient.CoreV1().Services(dataplane.Namespace).Get(ctx, dataplaneProxyService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, time.Minute, time.Second)
	verifyConnectivity(t, dataplaneIP)

	t.Log("verifying dataplane status is properly filled with backing service name and its addresses")
	require.Eventually(t, testutils.DataPlaneHasServiceAndAddressesInStatus(t, ctx, dataplaneName, clients), time.Minute, time.Second)

	t.Log("updating dataplane spec with proxy service type of ClusterIP")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, ctx, dataplaneName, clients, func(dp *operatorv1alpha1.DataPlane) { dp.Spec.Services.Proxy.Type = corev1.ServiceTypeClusterIP }),
		time.Minute, time.Second)

	t.Log("checking if dataplane proxy service type changes to ClusterIP")
	require.Eventually(t, func() bool {
		servicesClient := clients.K8sClient.CoreV1().Services(dataplane.Namespace)
		dataplaneProxyService, err := servicesClient.Get(ctx, dataplaneProxyService.Name, metav1.GetOptions{})
		if err != nil {
			t.Logf("error getting dataplane proxy service: %v", err)
			return false
		}
		if dataplaneProxyService.Spec.Type != corev1.ServiceTypeClusterIP {
			t.Logf("dataplane proxy service should be of ClusterIP type but is %s", dataplaneProxyService.Spec.Type)
			return false
		}

		return true
	}, time.Minute, time.Second)
}

func verifyConnectivity(t *testing.T, dataplaneIP string) {
	t.Log("verifying connectivity to the dataplane")
	resp, err := httpc.Get(fmt.Sprintf("https://%s", dataplaneIP))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, resp.StatusCode, http.StatusNotFound)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"message":"no Route matched with those values"`) // TODO: https://github.com/Kong/gateway-operator/issues/835
}

func TestDataPlaneUpdate(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &v1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: v1alpha1.DataPlaneSpec{
			DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Env: []corev1.EnvVar{
							{Name: "TEST_ENV", Value: "before_update"},
							{Name: consts.EnvVarKongDatabase, Value: "off"},
						},
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
	}
	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying that the dataplane gets marked as provisioned")
	require.Eventually(t, testutils.DataPlaneIsProvisioned(
		t, ctx, dataplaneName, clients.OperatorClient),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t,
		testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService corev1.Service
	require.Eventually(t,
		testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, clients),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
	require.Len(t, deployments, 1, "There must be only one DatePlane deployment")
	deployment := &deployments[0]

	t.Logf("verifying environment variable TEST_ENV in deployment before update")
	container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)
	testEnv := getEnvValueByName(container.Env, "TEST_ENV")
	require.Equal(t, "before_update", testEnv)

	t.Logf("updating dataplane resource")
	dataplane, err = dataplaneClient.Get(ctx, dataplane.Name, metav1.GetOptions{})
	require.NoError(t, err)
	dataplane.Spec.Deployment.Pods.Env = []corev1.EnvVar{
		{
			Name: "TEST_ENV", Value: "after_update",
		},
	}
	_, err = dataplaneClient.Update(ctx, dataplane, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Logf("verifying environment variable TEST_ENV in deployment after update")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
		require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
		deployment := &deployments[0]

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		testEnv := getEnvValueByName(container.Env, "TEST_ENV")
		t.Logf("Tenvironment variable TEST_ENV is now %s in deployment", testEnv)
		return testEnv == "after_update"
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	dataPlaneConditionPredicate := func(c *metav1.Condition) func(dataplane *v1alpha1.DataPlane) bool {
		return func(dataplane *v1alpha1.DataPlane) bool {
			for _, condition := range dataplane.Status.Conditions {
				if condition.Type == c.Type && condition.Status == c.Status {
					return true
				}
				t.Logf("DataPlane condition: Type=%q;Reason:%q;Status:%q;Message:%q",
					condition.Type, condition.Reason, condition.Status, condition.Message,
				)
			}
			return false
		}
	}

	var correctReadinessProbePath string
	t.Run("dataplane is not Ready when the underlying deployment changes state to not Ready", func(t *testing.T) {
		deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
		require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
		deployment := &deployments[0]
		require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
		container := &deployment.Spec.Template.Spec.Containers[0]
		correctReadinessProbePath = container.ReadinessProbe.HTTPGet.Path
		container.ReadinessProbe.HTTPGet.Path = "/status_which_will_always_return_404"
		_, err = env.Cluster().Client().AppsV1().Deployments(namespace.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		require.NoError(t, err)

		isNotReady := dataPlaneConditionPredicate(&metav1.Condition{
			Type:   string(k8sutils.ReadyType),
			Status: metav1.ConditionFalse,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, ctx, dataplaneName, isNotReady, clients.OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})
	t.Run("dataplane gets Ready when the underlying deployment changes state to Ready", func(t *testing.T) {
		deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
		require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
		deployment := &deployments[0]
		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		container.ReadinessProbe.HTTPGet.Path = correctReadinessProbePath
		_, err = env.Cluster().Client().AppsV1().Deployments(namespace.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		require.NoError(t, err)

		isReady := dataPlaneConditionPredicate(&metav1.Condition{
			Type:   string(k8sutils.ReadyType),
			Status: metav1.ConditionTrue,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, ctx, dataplaneName, isReady, clients.OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})
}

func TestDataPlaneHorizontalScaling(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("deploying dataplane resource with 2 replicas")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &v1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: v1alpha1.DataPlaneSpec{
			DataPlaneOptions: v1alpha1.DataPlaneOptions{
				Deployment: v1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(2)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
	}

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsProvisioned(t, ctx, dataplaneName, clients.OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients), time.Minute, time.Second)

	t.Log("verifying that dataplane has indeed 2 ready replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneName, clients, 2), time.Minute, time.Second)

	t.Log("changing replicas in dataplane spec to 1 should scale down the deployment back to 1")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, ctx, dataplaneName, clients, func(dp *operatorv1alpha1.DataPlane) { dp.Spec.Deployment.Replicas = lo.ToPtr(int32(1)) }),
		time.Minute, time.Second)

	t.Log("verifying that dataplane has indeed 1 ready replica after scaling down")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneName, clients, 1), time.Minute, time.Second)
}

func TestDataPlaneVolumeMounts(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("creating a secret to mount to dataplane containers")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      "test-secret",
		},
		StringData: map[string]string{
			"file-0": "foo",
		},
	}
	secret, err := clients.K8sClient.CoreV1().Secrets(namespace.Name).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(secret)

	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1alpha1.DataPlaneSpec{
			DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Volumes: []corev1.Volume{
							{
								Name: "test-volume",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: secret.Name,
									},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "test-volume",
								MountPath: "/var/test",
								ReadOnly:  true,
							},
						},
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
	}
	dataplane, err = clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name).Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying that the dataplane gets marked as provisioned")
	require.Eventually(t, testutils.DataPlaneIsProvisioned(t, ctx, dataplaneName, clients.OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients), time.Minute, time.Second)

	t.Log("verifying dataplane deployment volume mounts")
	deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)

	t.Log("verifying dataplane has default cluster-certificate volume")
	vol := getVolumeByName(deployment.Spec.Template.Spec.Volumes, consts.ClusterCertificateVolume)
	require.NotNil(t, vol, "dataplane pod should have the cluster-certificate volume")
	require.NotNil(t, vol.Secret, "cluster-certificate volume should come from secret")

	t.Log("verifying Kong proxy container has mounted  default cluster-certificate volume")
	volMounts := getVolumeMountsByVolumeName(proxyContainer.VolumeMounts, consts.ClusterCertificateVolume)
	require.Len(t, volMounts, 1, "proxy container should mount cluster-certificate volume")
	require.Equal(t, volMounts[0].MountPath, "/var/cluster-certificate", "proxy container should mount cluster-certificate volume to path /var/cluster-certificate")
	require.True(t, volMounts[0].ReadOnly, "proxy container should mount cluster-certificate volume in read only mode")

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

	t.Log("updating volumes and volume mounts in dataplane deployment to verify volumes could be reconciled")
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "test-volume-1",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secret.Name,
				},
			},
		},
	}
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{
			Name:      "test-volume-1",
			MountPath: "/var/test",
			ReadOnly:  true,
		},
	}
	_, err = clients.K8sClient.AppsV1().Deployments(namespace.Name).Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err, "should update deployment successfully")

	require.Eventually(t, func() bool {
		deployment, err = clients.K8sClient.AppsV1().Deployments(namespace.Name).Get(ctx, deployment.Name, metav1.GetOptions{})
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
