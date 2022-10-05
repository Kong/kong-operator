package test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/clientset"
)

// controlPlanePredicate is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a certain state.
func controlPlanePredicate(
	t *testing.T,
	ctx context.Context,
	controlplaneName types.NamespacedName,
	predicate func(controlplane *operatorv1alpha1.ControlPlane) bool,
	operatorClient *clientset.Clientset,
) func() bool {
	controlplaneClient := operatorClient.ApisV1alpha1().ControlPlanes(controlplaneName.Namespace)
	return func() bool {
		controlplane, err := controlplaneClient.Get(ctx, controlplaneName.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return predicate(controlplane)
	}
}

// DataPlanePredicate is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a certain state.
func DataPlanePredicate(
	t *testing.T,
	ctx context.Context,
	dataplaneName types.NamespacedName,
	predicate func(dataplane *operatorv1alpha1.DataPlane) bool,
	operatorClient *clientset.Clientset,
) func() bool {
	dataPlaneClient := operatorClient.ApisV1alpha1().DataPlanes(dataplaneName.Namespace)
	return func() bool {
		dataplane, err := dataPlaneClient.Get(ctx, dataplaneName.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return predicate(dataplane)
	}
}

// ControlPlaneIsScheduled is a helper function for tests that returns a function
// that can be used to check if a ControlPlane was scheduled.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsScheduled(t *testing.T, ctx context.Context, controlplane types.NamespacedName, operatorClient *clientset.Clientset) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) {
				return true
			}
		}
		return false
	}, operatorClient)
}

// ControlPlaneDetectedNoDataplane is a helper function for tests that returns a function
// that can be used to check if a ControlPlane detected unset dataplane.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneDetectedNoDataplane(t *testing.T, ctx context.Context, controlplane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) &&
				condition.Status == metav1.ConditionFalse &&
				condition.Reason == string(controllers.ControlPlaneConditionReasonNoDataplane) {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneIsProvisioned is a helper function for tests that returns a function
// that can be used to check if a ControlPlane was provisioned.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsProvisioned(t *testing.T, ctx context.Context, controlplane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) &&
				condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneIsNotReady is a helper function for tests. It returns a function
// that can be used to check if a ControlPlane is marked as not Ready.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsNotReady(t *testing.T, ctx context.Context, controlplane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(k8sutils.ReadyType) &&
				condition.Status == metav1.ConditionFalse {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneIsReady is a helper function for tests. It returns a function
// that can be used to check if a ControlPlane is marked as Ready.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsReady(t *testing.T, ctx context.Context, controlplane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *operatorv1alpha1.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(k8sutils.ReadyType) &&
				condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneHasActiveDeployment is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has an active deployment.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasActiveDeployment(t *testing.T, ctx context.Context, controlplaneName types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplaneName, func(controlplane *operatorv1alpha1.ControlPlane) bool {
		deployments := MustListControlPlaneDeployments(t, ctx, controlplane, clients)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas == *deployments[0].Spec.Replicas
	}, clients.OperatorClient)
}

// ControlPlaneHasClusterRole is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a ClusterRole.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasClusterRole(t *testing.T, ctx context.Context, controlplane *operatorv1alpha1.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		clusterRoles := MustListControlPlaneClusterRoles(t, ctx, controlplane, clients)
		t.Logf("%d clusterroles", len(clusterRoles))
		return len(clusterRoles) > 0
	}
}

// ControlPlaneHasClusterRoleBinding is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a ClusterRoleBinding.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasClusterRoleBinding(t *testing.T, ctx context.Context, controlplane *operatorv1alpha1.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		clusterRoleBindings := MustListControlPlaneClusterRoleBindings(t, ctx, controlplane, clients)
		t.Logf("%d clusterrolebindings", len(clusterRoleBindings))
		return len(clusterRoleBindings) > 0
	}
}

// DataPlaneHasActiveDeployment is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active deployment.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasActiveDeployment(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, clients K8sClients) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		deployments := MustListDataPlaneDeployments(t, ctx, dataplane, clients)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas == *deployments[0].Spec.Replicas
	}, clients.OperatorClient)
}

// DataPlaneHasService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a service created.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasService(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, clients K8sClients) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		services := MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient)
		return len(services) == 1
	}, clients.OperatorClient)
}

// DataPlaneHasActiveService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active service.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasActiveService(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, ret *corev1.Service, clients K8sClients) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1alpha1.DataPlane) bool {
		services := MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient)
		if len(services) == 1 {
			if ret != nil {
				*ret = services[0]
			}
			return true
		}
		return false
	}, clients.OperatorClient)
}

// GatewayClassIsAccepted is a helper function for tests that returns a function
// that can be used to check if a GatewayClass is accepted.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func GatewayClassIsAccepted(t *testing.T, ctx context.Context, gatewayClassName string, clients K8sClients) func() bool {
	gatewayClasses := clients.GatewayClient.GatewayV1beta1().GatewayClasses()

	return func() bool {
		gwc, err := gatewayClasses.Get(context.Background(), gatewayClassName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		for _, cond := range gwc.Status.Conditions {
			if cond.Reason == string(gatewayv1beta1.GatewayClassConditionStatusAccepted) {
				if cond.ObservedGeneration == gwc.Generation {
					return true
				}
			}
		}
		return false
	}
}

// GatewayNotExist is a helper function for tests that returns a function
// to check a if gateway(with specified namespace and name) does not exist.
//
//	Should be used in conjunction with require.Eventually or assert.Eventually.
func GatewayNotExist(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		gateways := clients.GatewayClient.GatewayV1beta1().Gateways(gatewayNSN.Namespace)
		_, err := gateways.Get(ctx, gatewayNSN.Name, metav1.GetOptions{})
		if err != nil {
			return errors.IsNotFound(err)
		}
		return false
	}
}

func GatewayIsScheduled(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		return gatewayutils.IsScheduled(MustGetGateway(t, ctx, gatewayNSN, clients))
	}
}

func GatewayIsReady(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		return gatewayutils.IsReady(MustGetGateway(t, ctx, gatewayNSN, clients))
	}
}

func GatewayDataPlaneIsProvisioned(t *testing.T, ctx context.Context, gateway *gatewayv1beta1.Gateway, clients K8sClients) func() bool {
	return func() bool {
		dataplanes := MustListDataPlanesForGateway(t, ctx, gateway, clients)

		if len(dataplanes) == 1 {
			// if the dataplane DeletionTimestamp is set, the dataplane deletion has been requested.
			// Hence we cannot consider it as a provisioned valid dataplane.
			if dataplanes[0].DeletionTimestamp != nil {
				return false
			}
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

func GatewayControlPlaneIsProvisioned(t *testing.T, ctx context.Context, gateway *gatewayv1beta1.Gateway, clients K8sClients) func() bool {
	return func() bool {
		controlplanes := MustListControlPlanesForGateway(t, ctx, gateway, clients)

		if len(controlplanes) == 1 {
			// if the controlplane DeletionTimestamp is set, the controlplane deletion has been requested.
			// Hence we cannot consider it as a provisioned valid controlplane.
			if controlplanes[0].DeletionTimestamp != nil {
				return false
			}
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

// GatewayNetworkPoliciesExist is a helper function for tests that returns a function
// that can be used to check if a Gateway owns a networkpolicy.
// Should be used in conjunction with require.Eventually or assert.Eventually.
// Gateway object argument does need to exist in the cluster, thu, the function
// may be used with Not after the gateway has been deleted, to verify that
// the networkpolicy has been deleted too.
func GatewayNetworkPoliciesExist(t *testing.T, ctx context.Context, gateway *gatewayv1beta1.Gateway, clients K8sClients) func() bool {
	return func() bool {
		networkpolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		return len(networkpolicies) > 0
	}
}

func GatewayIpAddressExist(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		gateway := MustGetGateway(t, ctx, gatewayNSN, clients)
		if len(gateway.Status.Addresses) > 0 && *gateway.Status.Addresses[0].Type == gatewayv1beta1.IPAddressType {
			return true
		}
		return false
	}
}

func GetResponseBodyContains(t *testing.T, ctx context.Context, clients K8sClients, httpc http.Client, url string, responseContains string) func() bool {
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
