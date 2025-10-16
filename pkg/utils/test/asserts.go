package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	gatewayutils "github.com/kong/kong-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// MustListControlPlanesForGateway is a helper function for tests that
// conveniently lists all controlplanes managed by a given gateway.
func MustListControlPlanesForGateway(t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients) []gwtypes.ControlPlane {
	controlPlanes, err := gatewayutils.ListControlPlanesForGateway(ctx, clients.MgrClient, gateway)
	require.NoError(t, err)
	return controlPlanes
}

// MustListKonnectGatewayControlPlanesForGateway is a helper function for tests that
// conveniently lists all KonnectGatewayControlPlanes managed by a given Gateway.
func MustListKonnectGatewayControlPlanesForGateway(t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients) []konnectv1alpha2.KonnectGatewayControlPlane {
	konnectGatewayControlPlanes, err := gatewayutils.ListKonnectGatewayControlPlanesForGateway(ctx, clients.MgrClient, gateway)
	require.NoError(t, err)
	return konnectGatewayControlPlanes
}

// MustListNetworkPoliciesForGateway is a helper function for tests that
// conveniently lists all NetworkPolicies managed by a given gateway.
func MustListNetworkPoliciesForGateway(t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients) []networkingv1.NetworkPolicy {
	networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, clients.MgrClient, gateway)
	require.NoError(t, err)
	return networkPolicies
}

// MustListDataPlaneServices is a helper function for tests that
// conveniently lists all proxy services managed by a given dataplane.
// Accepts any Kubernetes object (local or external API type) that implements client.Object.
func MustListDataPlaneServices(t *testing.T, ctx context.Context, dataplane client.Object, mgrClient client.Client, matchingLabels client.MatchingLabels) []corev1.Service {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		mgrClient,
		dataplane.GetNamespace(),
		dataplane.GetUID(),
		matchingLabels,
	)
	require.NoError(t, err)
	return services
}

// MustListDataPlaneDeployments is a helper function for tests that
// conveniently lists all deployments managed by a given dataplane.
// Accepts any Kubernetes object (local or external API type) that implements client.Object.
func MustListDataPlaneDeployments(t *testing.T, ctx context.Context, dataplane client.Object, clients K8sClients, matchinglabels client.MatchingLabels) []appsv1.Deployment {
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		clients.MgrClient,
		dataplane.GetNamespace(),
		dataplane.GetUID(),
		matchinglabels,
	)
	require.NoError(t, err)
	return deployments
}

// MustListDataPlaneHPAs is a helper function for tests that
// conveniently lists all HPAs managed by a given dataplane.
// Accepts any Kubernetes object (local or external API type) that implements client.Object.
func MustListDataPlaneHPAs(t *testing.T, ctx context.Context, dataplane client.Object, clients K8sClients, matchinglabels client.MatchingLabels) []autoscalingv2.HorizontalPodAutoscaler {
	hpas, err := k8sutils.ListHPAsForOwner(
		ctx,
		clients.MgrClient,
		dataplane.GetNamespace(),
		dataplane.GetUID(),
		matchinglabels,
	)
	require.NoError(t, err)
	return hpas
}

// MustListDataPlanePodDisruptionBudgets is a helper function for tests that
// conveniently lists all PDBs managed by a given dataplane.
func MustListDataPlanePodDisruptionBudgets(
	t *testing.T,
	ctx context.Context,
	dataplane client.Object,
	clients K8sClients,
	matchinglabels client.MatchingLabels,
) []policyv1.PodDisruptionBudget {
	pdbs, err := k8sutils.ListPodDisruptionBudgetsForOwner(
		ctx,
		clients.MgrClient,
		dataplane.GetNamespace(),
		dataplane.GetUID(),
		matchinglabels,
	)
	require.NoError(t, err)
	return pdbs
}

// MustListServiceEndpointSlices is a helper function for tests that
// conveniently lists all endpointSlices related to a specific service.
func MustListServiceEndpointSlices(t *testing.T, ctx context.Context, serviceName types.NamespacedName, mgrClient client.Client) []discoveryv1.EndpointSlice {
	epSliceList := &discoveryv1.EndpointSliceList{}
	err := mgrClient.List(ctx, epSliceList, client.InNamespace(serviceName.Namespace), client.MatchingLabels{
		discoveryv1.LabelServiceName: serviceName.Name,
	})
	require.NoError(t, err)
	return epSliceList.Items
}

// MustListDataPlanesForGateway is a helper function for tests that
// conveniently lists all dataplanes managed by a given gateway.
func MustListDataPlanesForGateway(t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients) []operatorv1beta1.DataPlane {
	// List DataPlanes using the controller-runtime client to ensure we work with
	// the external API types expected by tests in this package.
	dpList := &operatorv1beta1.DataPlaneList{}
	require.NoError(t, clients.MgrClient.List(
		ctx,
		dpList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue},
	))

	result := make([]operatorv1beta1.DataPlane, 0, len(dpList.Items))
	for _, dp := range dpList.Items {
		if k8sutils.IsOwnedByRefUID(&dp, gateway.UID) {
			result = append(result, dp)
		}
	}
	return result
}

// MustGetGateway is a helper function for tests that conveniently gets a gateway by name.
// It will fail the test if getting the gateway fails.
func MustGetGateway(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) *gwtypes.Gateway {
	gateways := clients.GatewayClient.GatewayV1().Gateways(gatewayNSN.Namespace)
	gateway, err := gateways.Get(ctx, gatewayNSN.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return gateway
}

// MustGetGatewayClass is a helper function for tests that conveniently gets a gatewayclass by name.
// It will fail the test if getting the gatewayclass fails.
func MustGetGatewayClass(t *testing.T, ctx context.Context, gatewayClassName string, clients K8sClients) *gwtypes.GatewayClass {
	gatewayClasses := clients.GatewayClient.GatewayV1().GatewayClasses()
	gatewayClass, err := gatewayClasses.Get(ctx, gatewayClassName, metav1.GetOptions{})
	require.NoError(t, err)
	return gatewayClass
}
