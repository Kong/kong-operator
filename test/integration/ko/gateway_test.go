package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/kr/pretty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/gatewayapi"
	gatewayutils "github.com/kong/kong-operator/v2/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	"github.com/kong/kong-operator/v2/test/helpers/envs"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestGatewayEssentials(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	t.Log("deploying a GatewayClass resource")
	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass, err := integration.GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("setting up watch for Gateways")
	cl, err := client.NewWithWatch(integration.GetEnv().Cluster().Config(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err, "failed to setup a client for watching gateways")
	wGateway, err := cl.Watch(ctx, &gatewayv1.GatewayList{}, client.InNamespace(namespace.Name))
	require.NoError(t, err, "failed to start watching gateways")
	t.Cleanup(func() { wGateway.Stop() })

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
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNN, clients.MgrClient)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("verifying that the DataPlane becomes Ready")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	dataplanes := testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, dataplanes, 1)
	dataplane := dataplanes[0]

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	controlplanes := testutils.MustListControlPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, controlplanes, 1)
	controlplane := controlplanes[0]

	t.Run("checking NetworkPolicies", func(t *testing.T) {
		t.Log("verifying networkpolicies are created")
		require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	})

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+gatewayIPAddress), testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying GatewayClass has supportedFeatures set")
	requiredFeatures, err := gatewayapi.GetSupportedFeatures(consts.RouterFlavorTraditionalCompatible)
	require.NoError(t, err)
	require.Eventually(t, testutils.GatewayClassHasSupportedFeatures(t, ctx, string(gateway.Spec.GatewayClassName), clients, requiredFeatures.UnsortedList()...), testutils.SubresourceReadinessWait, time.Second)

	dataplaneClient := integration.GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	dataplaneNN := types.NamespacedName{Namespace: namespace.Name, Name: dataplane.Name}
	controlplaneClient := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().ControlPlanes(namespace.Name)

	t.Log("verifying that dataplane has 1 ready replica")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneNN, clients, 1), time.Minute, time.Second)

	t.Log("deleting controlplane")
	require.NoError(t, controlplaneClient.Delete(ctx, controlplane.Name, metav1.DeleteOptions{}))

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

	t.Log("verifying that the ControlPlane becomes provisioned again")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), testutils.GatewayReadyTimeLimit, time.Second)
	controlplanes = testutils.MustListControlPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, controlplanes, 1)
	controlplane = controlplanes[0]

	t.Log("verifying that the DataPlane becomes provisioned again")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gateway, clients), testutils.GatewayReadyTimeLimit, time.Second)
	dataplanes = testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, dataplanes, 1)
	dataplane = dataplanes[0]

	t.Log("verifying Gateway gets marked as Programmed again")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address again")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNN, clients.MgrClient)
	gatewayIPAddress = gateway.Status.Addresses[0].Value

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+gatewayIPAddress), testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService corev1.Service
	dataplaneName := types.NamespacedName{
		Namespace: dataplane.Namespace,
		Name:      dataplane.Name,
	}
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), time.Minute, time.Second)

	t.Log("deleting the dataplane service")
	require.NoError(t, integration.GetClients().MgrClient.Delete(ctx, &dataplaneService))

	t.Log("verifying services managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, clients, client.MatchingLabels{
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
		_, err := integration.GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name).Get(ctx, dataplane.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying that ControlPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().ControlPlanes(namespace.Name).Get(ctx, controlplane.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Run("checking NetworkPolicies", func(t *testing.T) {
		t.Log("verifying networkpolicies are deleted")
		require.Eventually(t,
			testutils.Not(testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients)),
			time.Minute, time.Second,
		)
	})

	t.Log("verifying that gateway itself is deleted")
	require.Eventually(t, testutils.GatewayNotExist(t, ctx, gatewayNN, clients), time.Minute, time.Second)
}

// TestGatewayMultiple checks essential Gateway behavior with multiple Gateways of the same class.
// Ensure DataPlanes only serve routes attached to their Gateway.
func TestGatewayMultiple(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	gatewayV1Client := integration.GetClients().GatewayClient.GatewayV1()

	t.Log("deploying a GatewayClass resource")
	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass, err := gatewayV1Client.GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resources")
	gatewayOneNN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gatewayTwoNN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gatewayOne := helpers.GenerateGateway(gatewayOneNN, gatewayClass)
	gatewayOne, err = gatewayV1Client.Gateways(namespace.Name).Create(ctx, gatewayOne, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayOne)
	gatewayTwo := helpers.GenerateGateway(gatewayTwoNN, gatewayClass)
	gatewayTwo, err = gatewayV1Client.Gateways(namespace.Name).Create(ctx, gatewayTwo, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayTwo)

	t.Log("verifying Gateways marked as Scheduled")
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayOneNN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayTwoNN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateways marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayOneNN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayOneNN, clients), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayTwoNN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayTwoNN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateways get an IP address")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayOneNN, clients), testutils.SubresourceReadinessWait, time.Second)
	gatewayOne = testutils.MustGetGateway(t, ctx, gatewayOneNN, clients.MgrClient)
	gatewayOneIPAddress := gatewayOne.Status.Addresses[0].Value
	gatewayTwo = testutils.MustGetGateway(t, ctx, gatewayTwoNN, clients.MgrClient)
	gatewayTwoIPAddress := gatewayTwo.Status.Addresses[0].Value

	t.Log("verifying that the DataPlanes become Ready")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gatewayOne, clients), testutils.SubresourceReadinessWait, time.Second)
	dataplanesOne := testutils.MustListDataPlanesForGateway(t, ctx, gatewayOne, clients)
	require.Len(t, dataplanesOne, 1)
	dataplaneOne := dataplanesOne[0]
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gatewayTwo, clients), testutils.SubresourceReadinessWait, time.Second)
	dataplanesTwo := testutils.MustListDataPlanesForGateway(t, ctx, gatewayTwo, clients)
	require.Len(t, dataplanesTwo, 1)
	dataplaneTwo := dataplanesTwo[0]

	t.Log("verifying that the ControlPlanes become provisioned")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gatewayOne, clients), testutils.SubresourceReadinessWait, time.Second)
	controlplanesOne := testutils.MustListControlPlanesForGateway(t, ctx, gatewayOne, clients)
	require.Len(t, controlplanesOne, 1)
	controlplaneOne := controlplanesOne[0]
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gatewayTwo, clients), testutils.SubresourceReadinessWait, time.Second)
	controlplanesTwo := testutils.MustListControlPlanesForGateway(t, ctx, gatewayTwo, clients)
	require.Len(t, controlplanesTwo, 1)
	controlplaneTwo := controlplanesTwo[0]

	dataplaneOneNN := types.NamespacedName{Namespace: namespace.Name, Name: dataplaneOne.Name}
	dataplaneTwoNN := types.NamespacedName{Namespace: namespace.Name, Name: dataplaneTwo.Name}

	t.Log("verifying that dataplanes have 1 ready replica each")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneOneNN, clients, 1), time.Minute, time.Second)
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneTwoNN, clients, 1), time.Minute, time.Second)

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+gatewayOneIPAddress), testutils.SubresourceReadinessWait, time.Second)
	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+gatewayTwoIPAddress), testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying services are managed by their dataplanes")
	var dataplaneOneService corev1.Service
	dataplaneOneName := types.NamespacedName{
		Namespace: dataplaneOne.Namespace,
		Name:      dataplaneOne.Name,
	}
	var dataplaneTwoService corev1.Service
	dataplaneTwoName := types.NamespacedName{
		Namespace: dataplaneTwo.Namespace,
		Name:      dataplaneTwo.Name,
	}

	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneOneName, &dataplaneOneService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), time.Minute, time.Second)
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneTwoName, &dataplaneTwoService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), time.Minute, time.Second)

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("creating httproutes to access deployment %s via kong", deployment.Name)
	createRoute := func(httproute *gatewayv1.HTTPRoute) func(c *assert.CollectT) {
		return func(c *assert.CollectT) {
			result, err := gatewayV1Client.HTTPRoutes(namespace.Name).Create(ctx, httproute, metav1.CreateOptions{})
			require.NoErrorf(t, err, "failed to create HTTPRoute %s/%s", httproute.Namespace, httproute.Name)
			cleaner.Add(result)
		}
	}
	const pathOne = "/path-test-one"
	httpRouteOne := createHTTPRoute(gatewayOne, service, pathOne)
	require.EventuallyWithT(t, createRoute(httpRouteOne), 30*time.Second, time.Second)
	const pathTwo = "/path-test-two"
	httpRouteTwo := createHTTPRoute(gatewayTwo, service, pathTwo)
	require.EventuallyWithT(t, createRoute(httpRouteTwo), 30*time.Second, time.Second)

	t.Log("verifying connectivity to HTTPRoutes")

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	checkPaths := func(gatewayIpAddress, goodPath, badPath string) func(t *assert.CollectT) {
		return func(t *assert.CollectT) {
			url := fmt.Sprintf("http://%s%s", gatewayIpAddress, goodPath)
			bad := fmt.Sprintf("http://%s%s", gatewayIpAddress, badPath)

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			require.NoError(t, err)
			resp, err := httpClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			badReq, err := http.NewRequestWithContext(ctx, http.MethodGet, bad, nil)
			require.NoError(t, err)
			badResp, err := httpClient.Do(badReq)
			require.NoError(t, err)
			defer badResp.Body.Close()
			assert.Equal(t, http.StatusNotFound, badResp.StatusCode)
		}
	}

	require.EventuallyWithT(t, checkPaths(gatewayOneIPAddress, pathOne, pathTwo), time.Minute, time.Second)
	require.EventuallyWithT(t, checkPaths(gatewayTwoIPAddress, pathTwo, pathOne), time.Minute, time.Second)

	t.Log("deleting Gateway resource")
	require.NoError(t, gatewayV1Client.Gateways(namespace.Name).Delete(ctx, gatewayOne.Name, metav1.DeleteOptions{}))
	require.NoError(t, gatewayV1Client.Gateways(namespace.Name).Delete(ctx, gatewayTwo.Name, metav1.DeleteOptions{}))

	t.Log("verifying that DataPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := integration.GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name).Get(ctx, dataplaneOne.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)
	assert.Eventually(t, func() bool {
		_, err := integration.GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name).Get(ctx, dataplaneTwo.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying that ControlPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().ControlPlanes(namespace.Name).Get(ctx, controlplaneOne.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)
	assert.Eventually(t, func() bool {
		_, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().ControlPlanes(namespace.Name).Get(ctx, controlplaneTwo.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying that gateways are deleted")
	require.Eventually(t, testutils.GatewayNotExist(t, ctx, gatewayOneNN, clients), time.Minute, time.Second)
	require.Eventually(t, testutils.GatewayNotExist(t, ctx, gatewayTwoNN, clients), time.Minute, time.Second)
}

func createHTTPRoute(parentRef metav1.Object, svc metav1.Object, path string) *gatewayv1.HTTPRoute {
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: parentRef.GetNamespace(),
			Name:      uuid.NewString(),
			Annotations: map[string]string{
				"konghq.com/strip-path": "true",
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name: gatewayv1.ObjectName(parentRef.GetName()),
				}},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  new(gatewayv1.PathMatchPathPrefix),
								Value: new(path),
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(svc.GetName()),
									Port: new(gatewayv1.PortNumber(80)),
									Kind: new(gatewayv1.Kind("Service")),
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestGatewayWithMultipleListeners(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	t.Log("deploying a GatewayClass resource")
	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass, err := clients.GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gatewayNN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	const port8080 = 8080
	gateway := helpers.GenerateGateway(gatewayNN, gatewayClass, func(gateway *gatewayv1.Gateway) {
		gateway.Spec.Listeners = append(gateway.Spec.Listeners,
			gatewayv1.Listener{
				Name:     "http2",
				Protocol: gatewayv1.HTTPProtocolType,
				Port:     gatewayv1.PortNumber(port8080),
			},
		)
	})
	gateway, err = clients.GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets the IP addresses")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNN, clients.MgrClient)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("verifying that the DataPlane becomes Ready")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	dataplanes := testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, dataplanes, 1)
	dataplane := dataplanes[0]
	dataplaneNN := types.NamespacedName{Namespace: namespace.Name, Name: dataplane.Name}

	t.Log("verifying that dataplane has 1 ready replica")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneNN, clients, 1), time.Minute, time.Second)

	t.Run("checking NetworkPolicies", func(t *testing.T) {
		t.Log("verifying networkpolicies are created")
		require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	})

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, fmt.Sprintf("http://%s:80", gatewayIPAddress)), testutils.SubresourceReadinessWait, time.Second)
	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, fmt.Sprintf("http://%s:%d", gatewayIPAddress, port8080)), testutils.SubresourceReadinessWait, time.Second)
}

func TestScalingDataPlaneThroughGatewayConfiguration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	cl := integration.GetClients().MgrClient

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	require.NoError(t, cl.Create(ctx, gatewayConfig))
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass.Spec.ParametersRef = &gatewayv1.ParametersReference{
		Group:     "gateway-operator.konghq.com",
		Kind:      "GatewayConfiguration",
		Name:      gatewayConfig.Name,
		Namespace: (*gatewayv1.Namespace)(&namespace.Name),
	}
	t.Logf("deploying the GatewayClass %s", gatewayClass.Name)
	require.NoError(t, cl.Create(ctx, gatewayClass))
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gatewayNN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gateway := helpers.GenerateGateway(gatewayNN, gatewayClass)
	require.NoError(t, cl.Create(ctx, gateway))
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying that the DataPlane becomes ready")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)

	testCases := []struct {
		name                       string
		dataplaneDeploymentOptions operatorv2beta1.DeploymentOptions
		expectedReplicasCount      int32
	}{
		{
			name: "replicas=2",
			dataplaneDeploymentOptions: operatorv2beta1.DeploymentOptions{
				Replicas: new(int32(2)),
			},
			expectedReplicasCount: 2,
		},
		{
			name: "replicas=0",
			dataplaneDeploymentOptions: operatorv2beta1.DeploymentOptions{
				Replicas: new(int32(0)),
			},
			expectedReplicasCount: 0,
		},
		{
			name: "replicas=1",
			dataplaneDeploymentOptions: operatorv2beta1.DeploymentOptions{
				Replicas: new(int32(1)),
			},
			expectedReplicasCount: 1,
		},
		{
			name: "horizontal scaling with minReplicas=2",
			dataplaneDeploymentOptions: operatorv2beta1.DeploymentOptions{
				Scaling: &operatorv2beta1.Scaling{
					HorizontalScaling: &operatorv2beta1.HorizontalScaling{
						MinReplicas: new(int32(2)),
						MaxReplicas: 4,
					},
				},
			},
			expectedReplicasCount: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			require.EventuallyWithT(t, func(c *assert.CollectT) {
				deploymentOptions := tc.dataplaneDeploymentOptions
				var gatewayConfiguration operatorv2beta1.GatewayConfiguration
				require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: gatewayConfig.Name}, &gatewayConfiguration))
				gatewayConfiguration.Spec.DataPlaneOptions.Deployment.DeploymentOptions = deploymentOptions
				t.Logf("changing the GatewayConfiguration to change dataplane deploymentOptions to %v", deploymentOptions)
				err := cl.Update(ctx, &gatewayConfiguration)
				if !assert.NoError(c, err) {
					return
				}
			}, time.Minute, time.Second)

			t.Logf("verifying the deployment managed by the dataplane is ready and has %d available dataplane replicas", tc.expectedReplicasCount)
			dataplanes := testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)
			require.Len(t, dataplanes, 1)
			dataplane := dataplanes[0]
			dataplaneNN := client.ObjectKeyFromObject(&dataplane)
			require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t,
				ctx,
				dataplaneNN,
				&appsv1.Deployment{},
				client.MatchingLabels{
					consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
				},
				clients), testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)
			require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneNN, clients, tc.expectedReplicasCount), time.Minute, time.Second)
		})
	}
}

func TestGatewayDataPlaneNetworkPolicy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	var err error
	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err = integration.GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource")
	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass.Spec.ParametersRef = &gatewayv1.ParametersReference{
		Group:     "gateway-operator.konghq.com",
		Kind:      "GatewayConfiguration",
		Name:      gatewayConfig.Name,
		Namespace: (*gatewayv1.Namespace)(&namespace.Name),
	}
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
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the DataPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayDataPlaneIsReady(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	dataplanes := testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, dataplanes, 1)
	dataplane := dataplanes[0]

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	controlplanes := testutils.MustListControlPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, controlplanes, 1)

	t.Log("verifying DataPlane's NetworkPolicies is created")
	require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	networkpolicies := testutils.MustListNetworkPoliciesForGateway(t, ctx, gateway, clients)
	require.Len(t, networkpolicies, 1)
	networkPolicy := networkpolicies[0]
	require.Equal(t, map[string]string{"app": dataplane.Name}, networkPolicy.Spec.PodSelector.MatchLabels)

	t.Log("verifying that the DataPlane's Pod Admin API is network restricted to ControlPlane Pods")
	var expectLimitedAdminAPI networkPolicyIngressRuleDecorator
	expectLimitedAdminAPI.withTCPPort(consts.DataPlaneAdminAPIPort)
	// The controller restricts admin API access to kong-operator pods in kong-system namespace.
	// Uses app.kubernetes.io/name label to match telepresence traffic-manager pod labels.
	expectLimitedAdminAPI.withPeerMatchLabels(
		map[string]string{"app.kubernetes.io/name": "kong-operator"},
		map[string]string{"kubernetes.io/metadata.name": "kong-system"},
	)

	t.Log("verifying that the DataPlane's proxy ingress traffic is allowed")
	var expectAllowProxyIngress networkPolicyIngressRuleDecorator
	expectAllowProxyIngress.withTCPPort(consts.DataPlaneProxyPort)
	expectAllowProxyIngress.withTCPPort(consts.DataPlaneProxySSLPort)

	t.Log("verifying that the DataPlane's metrics ingress traffic is allowed")
	var expectAllowMetricsIngress networkPolicyIngressRuleDecorator
	expectAllowMetricsIngress.withTCPPort(consts.DataPlaneMetricsPort)

	t.Log("verifying DataPlane's NetworkPolicies ingress rules correctness")
	require.Contains(t, networkPolicy.Spec.Ingress, expectLimitedAdminAPI.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowProxyIngress.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowMetricsIngress.Rule)

	t.Log("deleting DataPlane's NetworkPolicies")
	require.NoError(t,
		integration.GetClients().K8sClient.NetworkingV1().
			NetworkPolicies(networkPolicy.Namespace).
			Delete(ctx, networkPolicy.Name, metav1.DeleteOptions{}),
	)

	t.Log("verifying NetworkPolicies are recreated")
	require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	networkpolicies = testutils.MustListNetworkPoliciesForGateway(t, ctx, gateway, clients)
	require.Len(t, networkpolicies, 1)
	networkPolicy = networkpolicies[0]
	t.Logf("NetworkPolicy generation %d", networkPolicy.Generation)

	t.Log("verifying DataPlane's NetworkPolicies ingress rules correctness")
	require.Contains(t, networkPolicy.Spec.Ingress, expectLimitedAdminAPI.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowProxyIngress.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowMetricsIngress.Rule)

	t.Run("verifying DataPlane's NetworkPolicies get updated after customizing kong proxy listen port through GatewayConfiguration", func(t *testing.T) {
		gwcClient := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name)
		t.Log("ingress rules get updated with configured admin listen port")
		setGatewayConfigurationEnvAdminAPIPort(t, gatewayConfig, 8555)
		_, err = gwcClient.Update(ctx, gatewayConfig, metav1.UpdateOptions{})
		require.NoError(t, err)

		var expectedUpdatedLimitedAdminAPI networkPolicyIngressRuleDecorator
		expectedUpdatedLimitedAdminAPI.withTCPPort(8555)
		expectedUpdatedLimitedAdminAPI.withPeerMatchLabels(
			map[string]string{"app.kubernetes.io/name": "kong-operator"},
			map[string]string{"kubernetes.io/metadata.name": "kong-system"},
		)
		if !assert.Eventually(t,
			testutils.GatewayNetworkPolicyForGatewayContainsRules(t, ctx, gateway, clients, expectedUpdatedLimitedAdminAPI.Rule),
			2*testutils.SubresourceReadinessWait, time.Second,
			"NetworkPolicy didn't get updated with port 8555 after a corresponding change to GatewayConfiguration") {
			networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, integration.GetClients().MgrClient, gateway)
			require.NoError(t, err)
			t.Log("DataPlane's NetworkPolicies")
			for _, np := range networkPolicies {
				t.Logf("%# v\n", pretty.Formatter(np))
			}
			t.FailNow()
		}

		var notExpectedUpdatedLimitedAdminAPI networkPolicyIngressRuleDecorator
		notExpectedUpdatedLimitedAdminAPI.withTCPPort(consts.DataPlaneAdminAPIPort)
		notExpectedUpdatedLimitedAdminAPI.withPeerMatchLabels(
			map[string]string{"app.kubernetes.io/name": "kong-operator"},
			map[string]string{"kubernetes.io/metadata.name": "kong-system"},
		)
		require.Eventually(t,
			testutils.Not(testutils.GatewayNetworkPolicyForGatewayContainsRules(t, ctx, gateway, clients, notExpectedUpdatedLimitedAdminAPI.Rule)),
			testutils.SubresourceReadinessWait, time.Second)
	})

	t.Run("verifying DataPlane's NetworkPolicies get deleted after Gateway is deleted", func(t *testing.T) {
		t.Log("deleting Gateway resource")
		require.NoError(t, integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))

		t.Log("verifying networkpolicies are deleted")
		require.Eventually(t, testutils.Not(testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients)), time.Minute, time.Second)
	})
}

func setGatewayConfigurationEnvAdminAPIPort(t *testing.T, gatewayConfiguration *operatorv2beta1.GatewayConfiguration, adminAPIPort int) {
	t.Helper()

	dpOptions := gatewayConfiguration.Spec.DataPlaneOptions
	if dpOptions == nil {
		dpOptions = &operatorv2beta1.GatewayConfigDataPlaneOptions{}
	}

	container := k8sutils.GetPodContainerByName(&dpOptions.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)

	container.Env = envs.SetValueByName(container.Env,
		"KONG_ADMIN_LISTEN",
		fmt.Sprintf("0.0.0.0:%d ssl reuseport backlog=16384", adminAPIPort),
	)

	gatewayConfiguration.Spec.DataPlaneOptions = dpOptions
}

type networkPolicyIngressRuleDecorator struct {
	Rule networkingv1.NetworkPolicyIngressRule
}

func (d *networkPolicyIngressRuleDecorator) withTCPPort(port int) {
	portIntStr := intstr.FromInt(port)
	protocol := corev1.ProtocolTCP
	d.Rule.Ports = append(d.Rule.Ports, networkingv1.NetworkPolicyPort{
		Protocol: &protocol,
		Port:     &portIntStr,
	})
}

func (d *networkPolicyIngressRuleDecorator) withPeerMatchLabels(
	podSelector map[string]string,
	namespaceSelector map[string]string,
) {
	d.Rule.From = append(d.Rule.From, networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: podSelector,
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: namespaceSelector,
		},
	})
}
