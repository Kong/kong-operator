package konnect

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/gatewayapi"
	gatewayutils "github.com/kong/kong-operator/v2/pkg/utils/gateway"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestGatewayHybridFull(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cl := integration.GetClients().MgrClient
	operatorClient := integration.GetClients().OperatorClient

	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	// Generate a test ID for labeling resources in order to easily identify them in Konnect.
	testID := uuid.NewString()[:8]
	t.Logf("Test ID: %s", testID)

	// Create a KonnectAPIAuthConfiguration
	// using the token from the test environment
	// and the Konnect server URL from the test environment.
	authCfg := deploy.KonnectAPIAuthConfiguration(
		t,
		ctx,
		client.NewNamespacedClient(cl, namespace.Name),
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			authCfg := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
			authCfg.Spec.Type = konnectv1alpha1.KonnectAPIAuthTypeToken
			authCfg.Spec.Token = test.KonnectAccessToken()
			authCfg.Spec.ServerURL = test.KonnectServerURL()
		},
	)

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	gatewayConfig.Spec.Konnect = &operatorv2beta1.KonnectOptions{
		APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
			Name: authCfg.Name,
		},
	}
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := operatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("setting up watch for Gateways")
	clw, err := client.NewWithWatch(integration.GetEnv().Cluster().Config(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err, "failed to setup a client for watching gateways")
	wGateway, err := clw.Watch(ctx, &gatewayv1.GatewayList{}, client.InNamespace(namespace.Name))
	require.NoError(t, err, "failed to start watching gateways")
	t.Cleanup(func() { wGateway.Stop() })

	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass.Spec.ParametersRef = &gatewayv1.ParametersReference{
		Group:     "gateway-operator.konghq.com",
		Kind:      "GatewayConfiguration",
		Name:      gatewayConfig.Name,
		Namespace: (*gatewayv1.Namespace)(&namespace.Name),
	}
	t.Logf("deploying the GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = integration.GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gatewayNN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gateway := helpers.GenerateGateway(gatewayNN, gatewayClass)
	gateway, err = integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNN, integration.GetClients()), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, cl), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, integration.GetClients()), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNN, integration.GetClients()), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNN, cl)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("verifying that the DataPlane becomes Ready")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gateway, integration.GetClients()), testutils.SubresourceReadinessWait, time.Second)
	dataPlanes := testutils.MustListDataPlanesForGateway(t, ctx, gateway, integration.GetClients())
	require.Len(t, dataPlanes, 1)
	dataplane := dataPlanes[0]

	t.Log("verifying that the KonnectGatewayControlPlane becomes provisioned")
	require.Eventually(t, testutils.KonnectGatewayControlPlaneIsProgrammed(t, ctx, gateway, integration.GetClients()), testutils.SubresourceReadinessWait, time.Second)
	konnectGatewayControlPlanes := testutils.MustListKonnectGatewayControlPlanesForGateway(t, ctx, gateway, integration.GetClients())
	require.Len(t, konnectGatewayControlPlanes, 1)
	konnectGatewayControlPlane := konnectGatewayControlPlanes[0]

	t.Run("checking NetworkPolicies", func(t *testing.T) {
		t.Log("verifying networkpolicies are created")
		require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, integration.GetClients()), testutils.SubresourceReadinessWait, time.Second)
	})

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+gatewayIPAddress), testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying GatewayClass has supportedFeatures set")
	requiredFeatures, err := gatewayapi.GetSupportedFeatures(consts.RouterFlavorTraditionalCompatible)
	require.NoError(t, err)
	require.Eventually(t, testutils.GatewayClassHasSupportedFeatures(t, ctx, string(gateway.Spec.GatewayClassName), integration.GetClients(), requiredFeatures...), testutils.SubresourceReadinessWait, time.Second)

	dataplaneClient := operatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	dataplaneNN := types.NamespacedName{Namespace: namespace.Name, Name: dataplane.Name}

	t.Log("verifying that dataplane has 1 ready replica")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneNN, integration.GetClients(), 1), time.Minute, time.Second)

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	httpRoute := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name)
	t.Logf("creating HTTPRoute %s/%s to access deployment %s via kong", httpRoute.Namespace, httpRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := integration.GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(ctx, httpRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy HTTPRoute %s/%s", httpRoute.Namespace, httpRoute.Name)
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)
	cleaner.Add(httpRoute)

	verifyHTTPRoute := func(t *testing.T, gatewayIPAddress string) {
		t.Helper()
		t.Log("verifying connectivity to the HTTPRoute")
		const (
			httpRouteAccessTimeout = 3 * time.Minute
			waitTick               = time.Second
		)

		httpClient, err := helpers.CreateHTTPClient(nil, "")
		require.NoError(t, err)

		t.Log("route to /test path of service httpbin should receive a 200 OK response")
		request := helpers.MustBuildRequest(t, ctx, http.MethodGet, "http://"+gatewayIPAddress+"/test", "")
		require.Eventually(
			t,
			testutils.GetResponseBodyContains(t, httpClient, request, "<title>httpbin.org</title>"),
			httpRouteAccessTimeout,
			time.Second,
		)

		t.Log("route to /test/1234 path of service httpbin should receive a 404 OK response")
		request = helpers.MustBuildRequest(t, ctx, http.MethodGet, "http://"+gatewayIPAddress+"/test/1234", "")
		require.Eventually(
			t,
			testutils.GetResponseBodyContains(t, httpClient, request, "<h1>Not Found</h1>"),
			httpRouteAccessTimeout,
			time.Second,
		)
	}
	t.Log("verify HTTPRoute routing")
	verifyHTTPRoute(t, gatewayIPAddress)

	t.Log("deleting dataplane")
	require.NoError(t, dataplaneClient.Delete(ctx, dataplane.Name, metav1.DeleteOptions{}))

	// Since the `Programmed = False` condition of gateway and listeners is a transient state
	// which appears only for a short duration and recovers to `Programmed = True` after CP and DP restarted,
	// we use `watch` instead of `Eventually` to catch the state.
	t.Logf("verifying Gateway gets and its listeners are marked as not Programmed")
	_ = helpers.WatchFor(t, ctx, wGateway, apiwatch.Modified,
		testutils.GatewayReadyTimeLimit,
		func(gw *gatewayv1.Gateway) bool {
			return gw.Name == gatewayNN.Name && gw.Namespace == gatewayNN.Namespace &&
				!gatewayutils.IsProgrammed(gw) && !gatewayutils.AreListenersProgrammed(gw)
		},
		"Did not see gateway and all its listeners' Programmed condition set to False in watching gateways",
	)

	t.Log("verifying that the DataPlane becomes provisioned again")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gateway, integration.GetClients()), 45*time.Second, time.Second)
	dataPlanes = testutils.MustListDataPlanesForGateway(t, ctx, gateway, integration.GetClients())
	require.Len(t, dataPlanes, 1)
	dataplane = dataPlanes[0]

	t.Log("verifying Gateway gets marked as Programmed again")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, cl), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, integration.GetClients()), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying connectivity to the Gateway")
	// We're using eventually because Gateway can still have the stale IP address
	// from old DataPlane.
	require.Eventually(t, func() bool {
		gw := testutils.MustGetGateway(t, ctx, gatewayNN, cl)
		addresses := gw.Status.Addresses
		if len(addresses) == 0 ||
			addresses[0].Type == nil ||
			*addresses[0].Type != gatewayv1.IPAddressType {
			return false
		}
		gatewayIPAddress = addresses[0].Value
		return asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+gatewayIPAddress)()
	}, testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService corev1.Service
	dataplaneName := types.NamespacedName{
		Namespace: dataplane.Namespace,
		Name:      dataplane.Name,
	}
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, integration.GetClients(), client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), time.Minute, time.Second)

	t.Log("deleting the dataplane service")
	require.NoError(t, integration.GetClients().MgrClient.Delete(ctx, &dataplaneService))

	t.Log("verifying services managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, integration.GetClients(), client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), time.Minute, time.Second)
	services := testutils.MustListDataPlaneServices(t, ctx, &dataplane, integration.GetClients().MgrClient, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	})
	require.Len(t, services, 1)

	t.Log("deleting Gateway resource")
	require.NoError(t, integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))

	t.Log("verifying that DataPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := operatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name).Get(ctx, dataplane.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying that KonnectGatewayControlPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := operatorClient.KonnectV1alpha2().KonnectGatewayControlPlanes(namespace.Name).Get(ctx, konnectGatewayControlPlane.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Run("checking NetworkPolicies", func(t *testing.T) {
		t.Log("verifying networkpolicies are deleted")
		require.Eventually(t,
			testutils.Not(testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, integration.GetClients())),
			time.Minute, time.Second,
		)
	})

	t.Log("verifying that gateway itself is deleted")
	require.Eventually(t, testutils.GatewayNotExist(t, ctx, gatewayNN, integration.GetClients()), time.Minute, time.Second)
}
