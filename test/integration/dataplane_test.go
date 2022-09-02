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
	"github.com/kong/gateway-operator/controllers"
	"github.com/kong/gateway-operator/internal/consts"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
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

	controllerContainer := k8sresources.GetPodContainerByName(
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
