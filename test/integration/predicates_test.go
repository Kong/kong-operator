//go:build integration_tests
// +build integration_tests

package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
)

// controlPlanePredicate is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a certain state.
func controlPlanePredicate(
	t *testing.T,
	controlplaneName types.NamespacedName,
	predicate func(controlplane *operatorv1alpha1.ControlPlane) bool,
) func() bool {
	controlplaneClient := operatorClient.ApisV1alpha1().ControlPlanes(controlplaneName.Namespace)
	return func() bool {
		controlplane, err := controlplaneClient.Get(ctx, controlplaneName.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return predicate(controlplane)
	}
}

// dataPlanePredicate is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a certain state.
func dataPlanePredicate(
	t *testing.T,
	dataplaneName types.NamespacedName,
	predicate func(dataplane *operatorv1alpha1.DataPlane) bool,
) func() bool {
	dataPlaneClient := operatorClient.ApisV1alpha1().DataPlanes(dataplaneName.Namespace)
	return func() bool {
		dataplane, err := dataPlaneClient.Get(ctx, dataplaneName.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return predicate(dataplane)
	}
}

// controlPlaneIsScheduled is a helper function for tests that returns a function
// that can be used to check if a ControlPlane was scheduled.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func controlPlaneIsScheduled(t *testing.T, controlplane types.NamespacedName) func() bool {
	return controlPlanePredicate(t, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) {
				return true
			}
		}
		return false
	})
}

// controlPlaneDetectedNoDataplane is a helper function for tests that returns a function
// that can be used to check if a ControlPlane detected unset dataplane.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func controlPlaneDetectedNoDataplane(t *testing.T, controlplane types.NamespacedName) func() bool {
	return controlPlanePredicate(t, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) &&
				condition.Status == metav1.ConditionFalse &&
				condition.Reason == string(controllers.ControlPlaneConditionReasonNoDataplane) {
				return true
			}
		}
		return false
	})
}

// controlPlaneIsProvisioned is a helper function for tests that returns a function
// that can be used to check if a ControlPlane was provisioned.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func controlPlaneIsProvisioned(t *testing.T, controlplane types.NamespacedName) func() bool {
	return controlPlanePredicate(t, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) &&
				condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	})
}

// controlPlaneHasActiveDeployment is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has an active deployment.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func controlPlaneHasActiveDeployment(t *testing.T, controlplaneName types.NamespacedName) func() bool {
	return controlPlanePredicate(t, controlplaneName, func(controlplane *operatorv1alpha1.ControlPlane) bool {
		deployments := mustListControlPlaneDeployments(t, controlplane)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
	})
}

// dataPlaneHasActiveDeployment is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active deployment.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func dataPlaneHasActiveDeployment(t *testing.T, dataplaneName types.NamespacedName) func() bool {
	return dataPlanePredicate(t, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		deployments := mustListDataPlaneDeployments(t, dataplane)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
	})
}

// dataPlaneHasService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a service created.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func dataPlaneHasService(t *testing.T, dataplaneName types.NamespacedName) func() bool {
	return dataPlanePredicate(t, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		services := mustListDataPlaneServices(t, dataplane)
		if len(services) == 1 {
			return true
		}
		return false
	})
}

// dataPlaneHasActiveService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active service.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func dataPlaneHasActiveService(t *testing.T, dataplaneName types.NamespacedName, ret *corev1.Service) func() bool {
	return dataPlanePredicate(t, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		services := mustListDataPlaneServices(t, dataplane)
		if len(services) == 1 {
			if ret != nil {
				*ret = services[0]
			}
			return true
		}
		return false
	})
}

// Not is a helper function for tests that returns a negation of a predicate.
func Not(predicate func() bool) func() bool {
	return func() bool {
		return !predicate()
	}
}
