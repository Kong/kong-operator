//go:build integration_tests
// +build integration_tests

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
)

func TestControlPlaneEssentials(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	dataplaneClient := operatorClient.V1alpha1().DataPlanes(namespace.Name)
	controlplaneClient := operatorClient.V1alpha1().ControlPlanes(namespace.Name)

	// Control plane needs a dataplane to exist to properly function.
	dataplane := &v1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
	}

	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			ControlPlaneDeploymentOptions: operatorv1alpha1.ControlPlaneDeploymentOptions{
				DataPlane: &dataplane.Name,
			},
		},
	}

	t.Log("deploying dataplane resource")
	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, func() bool {
		deployments := mustListDataPlaneDeployments(t, dataplane)
		return len(deployments) == 1 &&
			deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
	}, time.Minute, time.Second)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, func() bool {
		services := mustListDataPlaneServices(t, dataplane)
		if len(services) == 1 {
			return true
		}
		return false
	}, time.Minute, time.Second)

	t.Log("deploying controlplane resource")
	controlplane, err = controlplaneClient.Create(ctx, controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying controlplane gets marked scheduled")
	isScheduled := func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) {
				return true
			}
		}
		return false
	}
	require.Eventually(t, controlPlanePredicate(t, controlplane.Namespace, controlplane.Name, isScheduled), time.Minute, time.Second)

	t.Log("verifying that the controlplane gets marked as provisioned")
	isProvisioned := func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) &&
				condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}
	require.Eventually(t, controlPlanePredicate(t, controlplane.Namespace, controlplane.Name, isProvisioned), 2*time.Minute, time.Second)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, func() bool {
		deployments := mustListControlPlaneDeployments(t, controlplane)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
	}, time.Minute, time.Second)
}
