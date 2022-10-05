//go:build integration_tests
// +build integration_tests

package integration

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
)

func TestDataplaneEssentials(t *testing.T) {
	namespace, cleaner := setup(t, ctx, env, clients)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

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
			DataPlaneDeploymentOptions: v1alpha1.DataPlaneDeploymentOptions{
				DeploymentOptions: v1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{Name: "TEST_ENV", Value: "test"},
					},
				},
			},
		},
	}
	dataplane, err := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name).Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked scheduled")
	isScheduled := func(dataplane *v1alpha1.DataPlane) bool {
		for _, condition := range dataplane.Status.Conditions {
			if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) {
				return true
			}
		}
		return false
	}
	require.Eventually(t, testutils.DataPlanePredicate(t, ctx, dataplaneName, isScheduled, clients.OperatorClient), time.Minute, time.Second)

	t.Log("verifying that the dataplane gets marked as provisioned")
	isProvisioned := func(dataplane *v1alpha1.DataPlane) bool {
		for _, condition := range dataplane.Status.Conditions {
			if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}
	require.Eventually(t, testutils.DataPlanePredicate(t, ctx, dataplaneName, isProvisioned, clients.OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients), time.Minute, time.Second)

	// check environment variables of deployments and pods.

	t.Log("verifying dataplane deployment env vars")
	deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients)
	require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
	deployment := &deployments[0]

	controllerContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, controllerContainer)
	envs := controllerContainer.Env
	// check specified custom envs
	testEnvValue := getEnvValueByName(envs, "TEST_ENV")
	require.Equal(t, "test", testEnvValue)
	// check default envs added by operator
	kongDatabaseEnvValue := getEnvValueByName(envs, consts.EnvVarKongDatabase)
	require.Equal(t, "off", kongDatabaseEnvValue)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, clients), time.Minute, time.Second)

	t.Log("verifying dataplane services receive IP addresses")
	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := clients.K8sClient.CoreV1().Services(dataplane.Namespace).Get(ctx, dataplaneService.Name, metav1.GetOptions{})
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
	require.NoError(t, clients.MgrClient.Delete(ctx, &dataplaneService))

	t.Log("verifying services managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, clients), time.Minute, time.Second)

	t.Log("verifying dataplane services receive IP addresses after deletion")
	require.Eventually(t, func() bool {
		dataplaneService, err := clients.K8sClient.CoreV1().Services(dataplane.Namespace).Get(ctx, dataplaneService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, time.Minute, time.Second)

	verifyConnectivity(t, dataplaneIP)
}

func verifyConnectivity(t *testing.T, dataplaneIP string) {
	t.Log("verifying un-authenticated requests fail")
	badhttpc := http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		},
	}
	resp, err := badhttpc.Get(fmt.Sprintf("https://%s:8444/status", dataplaneIP))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, resp.StatusCode, http.StatusBadRequest)

	t.Log("verifying connectivity to the dataplane")
	resp, err = httpc.Get(fmt.Sprintf("https://%s:8444/status", dataplaneIP))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"database":{"reachable":true}`)
}

func TestDataPlaneUpdate(t *testing.T) {
	namespace, cleaner := setup(t, ctx, env, clients)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

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
			DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{Name: "TEST_ENV", Value: "before_update"},
						{Name: consts.EnvVarKongDatabase, Value: "off"},
					},
				},
			},
		},
	}
	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

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

	t.Log("verifying that the dataplane gets marked as provisioned")
	isProvisioned := dataPlaneConditionPredicate(&metav1.Condition{
		Type:   string(controllers.DataPlaneConditionTypeProvisioned),
		Status: metav1.ConditionTrue,
	})
	require.Eventually(t,
		testutils.DataPlanePredicate(t, ctx, dataplaneName, isProvisioned, clients.OperatorClient),
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
	dataplane.Spec.DeploymentOptions.Env = []corev1.EnvVar{
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
