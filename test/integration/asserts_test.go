//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// mustListDataPlaneDeployments is a helper function for tests that
// conveniently lists all deployments managed by a given dataplane.
func mustListDataPlaneDeployments(t *testing.T, dataplane *operatorv1alpha1.DataPlane) []appsv1.Deployment {
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		mgrClient,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	require.NoError(t, err)
	return deployments
}

// mustListControlPlaneDeployments is a helper function for tests that
// conveniently lists all deployments managed by a given controlplane.
func mustListControlPlaneDeployments(t *testing.T, controlplane *operatorv1alpha1.ControlPlane) []appsv1.Deployment {
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		mgrClient,
		consts.GatewayOperatorControlledLabel,
		consts.ControlPlaneManagedLabelValue,
		controlplane.Namespace,
		controlplane.UID,
	)
	require.NoError(t, err)
	return deployments
}

// mustListControlPlaneClusterRoles is a helper function for tests that
// conveniently lists all clusterroles owned by a given controlplane.
func mustListControlPlaneClusterRoles(t *testing.T, ctx context.Context, controlplane *operatorv1alpha1.ControlPlane) []rbacv1.ClusterRole {
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(
		ctx,
		mgrClient,
		consts.GatewayOperatorControlledLabel,
		consts.ControlPlaneManagedLabelValue,
		controlplane.UID,
	)
	require.NoError(t, err)
	return clusterRoles
}

// mustListControlPlaneClusterRoleBindings is a helper function for tests that
// conveniently lists all clusterrolebindings owned by a given controlplane.
func mustListControlPlaneClusterRoleBindings(t *testing.T, ctx context.Context, controlplane *operatorv1alpha1.ControlPlane) []rbacv1.ClusterRoleBinding {
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(
		ctx,
		mgrClient,
		consts.GatewayOperatorControlledLabel,
		consts.ControlPlaneManagedLabelValue,
		controlplane.UID,
	)
	require.NoError(t, err)
	return clusterRoleBindings
}

func mustListControlPlanesForGateway(t *testing.T, gateway *gatewayv1alpha2.Gateway) []operatorv1alpha1.ControlPlane {
	controlPlanes, err := gatewayutils.ListControlPlanesForGateway(ctx, mgrClient, gateway)
	require.NoError(t, err)
	return controlPlanes
}

func mustListNetworkPoliciesForGateway(t *testing.T, gateway *gatewayv1alpha2.Gateway) []networkingv1.NetworkPolicy { //nolint:unused,deadcode
	networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, mgrClient, gateway)
	require.NoError(t, err)
	return networkPolicies
}

// mustListServices is a helper function for tests that
// conveniently lists all services managed by a given dataplane.
func mustListDataPlaneServices(t *testing.T, dataplane *operatorv1alpha1.DataPlane) []corev1.Service {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		mgrClient,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	require.NoError(t, err)
	return services
}

func mustListDataPlanesForGateway(t *testing.T, ctx context.Context, gateway *gatewayv1alpha2.Gateway) []operatorv1alpha1.DataPlane {
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, mgrClient, gateway)
	require.NoError(t, err)
	return dataplanes
}

// mustGetGateway is a helper function for tests that conveniently gets a gateway by name.
// It will fail the test if getting the gateway fails.
func mustGetGateway(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName) *gatewayv1alpha2.Gateway {
	gateways := gatewayClient.GatewayV1alpha2().Gateways(gatewayNSN.Namespace)
	gateway, err := gateways.Get(ctx, gatewayNSN.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return gateway
}

func mustListGatewayNetworkPolicies(t *testing.T, ctx context.Context, gateway *gatewayv1alpha2.Gateway) []networkingv1.NetworkPolicy {
	networkpolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, mgrClient, gateway)
	require.NoError(t, err)
	return networkpolicies
}
