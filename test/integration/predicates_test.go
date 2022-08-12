//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
)

// controlPlanePredicate is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a certain state.
func controlPlanePredicate(
	t *testing.T,
	ctx context.Context,
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
	ctx context.Context,
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
func controlPlaneIsScheduled(t *testing.T, ctx context.Context, controlplane types.NamespacedName) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
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
func controlPlaneDetectedNoDataplane(t *testing.T, ctx context.Context, controlplane types.NamespacedName) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
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
func controlPlaneIsProvisioned(t *testing.T, ctx context.Context, controlplane types.NamespacedName) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
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
func controlPlaneHasActiveDeployment(t *testing.T, ctx context.Context, controlplaneName types.NamespacedName) func() bool {
	return controlPlanePredicate(t, ctx, controlplaneName, func(controlplane *operatorv1alpha1.ControlPlane) bool {
		deployments := mustListControlPlaneDeployments(t, controlplane)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
	})
}

// dataPlaneHasActiveDeployment is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active deployment.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func dataPlaneHasActiveDeployment(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName) func() bool {
	return dataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		deployments := mustListDataPlaneDeployments(t, dataplane)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
	})
}

// dataPlaneHasService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a service created.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func dataPlaneHasService(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName) func() bool {
	return dataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		services := mustListDataPlaneServices(t, dataplane)
		return len(services) == 1
	})
}

// dataPlaneHasActiveService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active service.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func dataPlaneHasActiveService(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, ret *corev1.Service) func() bool {
	return dataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
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

func gatewayIsScheduled(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName) func() bool {
	return func() bool {
		return gatewayutils.IsScheduled(mustGetGateway(t, ctx, gatewayNSN))
	}
}

func gatewayIsReady(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName) func() bool {
	return func() bool {
		return gatewayutils.IsReady(mustGetGateway(t, ctx, gatewayNSN))
	}
}

func gatewayDataPlaneIsProvisioned(t *testing.T, gateway *gatewayv1alpha2.Gateway) func() bool {
	return func() bool {
		dataplanes := mustListDataPlanesForGateway(t, ctx, gateway)

		if len(dataplanes) == 1 {
			for _, condition := range dataplanes[0].Status.Conditions {
				if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) &&
					condition.Status == metav1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}
}

func gatewayControlPlaneIsProvisioned(t *testing.T, gateway *gatewayv1alpha2.Gateway) func() bool {
	return func() bool {
		controlplanes := mustListControlPlanesForGateway(t, gateway)

		if len(controlplanes) == 1 {
			for _, condition := range controlplanes[0].Status.Conditions {
				if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) &&
					condition.Status == metav1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}
}

// gatewayNetworkPoliciesExist is a helper function for tests that returns a function
// that can be used to check if a Gateway owns a networkpolicy.
// Should be used in conjunction with require.Eventually or assert.Eventually.
// Gateway object argument does need to exist in the cluster, thu, the function
// may be used with Not after the gateway has been deleted, to verify that
// the networkpolicy has been deleted too.
func gatewayNetworkPoliciesExist(t *testing.T, ctx context.Context, gateway *gatewayv1alpha2.Gateway) func() bool { //nolint:unparam
	return func() bool {
		networkpolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		return len(networkpolicies) > 0
	}
}

func gatewayIpAddressExist(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName) func() bool {
	return func() bool {
		gateway := mustGetGateway(t, ctx, gatewayNSN)
		if len(gateway.Status.Addresses) > 0 && *gateway.Status.Addresses[0].Type == gatewayv1alpha2.IPAddressType {
			return true
		}
		return false
	}
}

func getResponseBodyContains(t *testing.T, ctx context.Context, url string, responseContains string) func() bool {
	return func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		require.NoError(t, err)

		resp, err := httpc.Do(req)
		if err != nil {
			return false
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		return strings.Contains(string(body), responseContains)
	}
}

// Not is a helper function for tests that returns a negation of a predicate.
func Not(predicate func() bool) func() bool {
	return func() bool {
		return !predicate()
	}
}
