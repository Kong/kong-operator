//go:build integration_tests
// +build integration_tests

package integration

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
)

func TestDataplaneEssentials(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	t.Log("deploying dataplane resource")
	dataplane := &v1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
	}
	dataplane, err := operatorClient.V1alpha1().DataPlanes(namespace.Name).Create(ctx, dataplane, metav1.CreateOptions{})
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
	require.Eventually(t, dataPlanePredicate(t, dataplane.Namespace, dataplane.Name, isScheduled), time.Minute, time.Second)

	t.Log("verifying that the dataplane gets marked as provisioned")
	isProvisioned := func(dataplane *v1alpha1.DataPlane) bool {
		for _, condition := range dataplane.Status.Conditions {
			if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}
	require.Eventually(t, dataPlanePredicate(t, dataplane.Namespace, dataplane.Name, isProvisioned), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, func() bool {
		deployments := mustListDataPlaneDeployments(t, dataplane)
		return len(deployments) == 1 && deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
	}, time.Minute, time.Second)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService *corev1.Service
	require.Eventually(t, func() bool {
		services := mustListDataPlaneServices(t, dataplane)
		if len(services) == 1 {
			dataplaneService = &services[0]
			return true
		}
		return false
	}, time.Minute, time.Second)

	t.Log("verifying dataplane services receive IP addresses")
	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := k8sClient.CoreV1().Services(dataplane.Namespace).Get(ctx, dataplaneService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, time.Minute, time.Second)

	t.Log("verifying connectivity to the dataplane")
	resp, err := httpc.Get(fmt.Sprintf("https://%s:8444/status", dataplaneIP))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"database":{"reachable":true}`)
}
