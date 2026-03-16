package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/kr/pretty"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

const (
	testHeaderKey   = "Header-Added-By-Plugin"
	testHeaderValue = "Test"
)

func TestHelmUpgrade(t *testing.T) {
	ctx := t.Context()

	// This is the latest Chart available publicly (used by actual users) that we can upgrade from.
	const (
		lastReleasedChart        = "oci://docker.io/kong/kong-operator-chart"
		lastReleasedChartVersion = "1.2.1" // renovate: datasource=docker depName=kong/kong-operator-chart versioning=docker
	)
	// This is the Chart and image from current state of the repository that we want to upgrade to.
	// Image has to be loaded into the cluster beforehand and specified via KONG_TEST_KONG_OPERATOR_IMAGE_LOAD
	// env var as a prerequisite.
	var currentChart = kcfg.ChartPath()
	t.Logf("KONG_TEST_KONG_OPERATOR_IMAGE_LOAD set to %q", imageLoad)
	currentImageRepository, currentImageTag := splitRepoVersionFromImageOrFail(t, imageLoad)

	// CreateEnvironment will queue up environment cleanup if necessary
	// and dumping diagnostics if the test fails.
	e := CreateEnvironment(t, ctx)

	// List of Kubernetes objects that should be present in the cluster
	// to check if they are properly handled during the upgrade.
	gatewayConfig := helpers.GenerateGatewayConfiguration(e.Namespace.Name)
	gatewayClassParametersRef := gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv2beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&e.Namespace.Name),
		Name:      gatewayConfig.Name,
	}
	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayClassParametersRef)
	gateway := helpers.GenerateGateway(
		types.NamespacedName{Namespace: e.Namespace.Name, Name: "gateway-on-prem"},
		gatewayClass,
		func(gw *gatewayv1.Gateway) {
			gw.Labels = map[string]string{"gateway-on-prem": "true"}
		},
	)
	// Create a test service backend for the HTTPRoute.
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	testDeployment := generators.NewDeploymentForContainer(container)
	testDeployment.Namespace = e.Namespace.Name
	testService := generators.NewServiceForDeployment(testDeployment, corev1.ServiceTypeClusterIP)
	testService.Namespace = e.Namespace.Name
	kongPlugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: e.Namespace.Name,
			Name:      "response-transformer-add-header",
		},
		PluginName: "response-transformer",
		Config: apiextensionsv1.JSON{
			Raw: fmt.Appendf(nil, `{"add":{"headers":["%s:%s"]}}`, testHeaderKey, testHeaderValue),
		},
	}
	httpRoute := helpers.GenerateHTTPRoute(
		e.Namespace.Name, gateway.Name, testService.Name, func(h *gatewayv1.HTTPRoute) {
			h.Annotations["konghq.com/plugins"] = kongPlugin.Name
		},
	)

	objectsToDeploy := []client.Object{
		gatewayConfig,
		gatewayClass,
		gateway,
		testDeployment,
		testService,
		kongPlugin,
		httpRoute,
	}

	// Assertion is run after the upgrade to assert the state of the resources in the cluster.
	type assertion struct {
		Name string
		Func func(*assert.CollectT, *testutils.K8sClients)
	}
	assertionsAfterInstall := []assertion{
		{
			Name: "Gateway is programmed",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				gatewayAndItsListenersAreProgrammedAssertion("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
		{
			Name: "ControlPlane is ready",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				controlPlaneOwnedByGatewayReady("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
		{
			Name: "DataPlane is ready",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				dataPlaneOwnedByGatewayReady("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},

		{
			Name: "DataPlane deployment is not patched after install",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				gatewayDataPlaneDeploymentIsNotPatched("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
		{
			Name: "HTTPRoute responds with 200 status code and presents response header added by plugin",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				gatewayHTTPRoutingWorks("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
	}
	// This is the place to add steps that should be performed before the upgrade.
	// For instance change in CRDs requires manual installation of new CRDs before the upgrade,
	// see charts/kong-operator/UPGRADE.md this section mostly should be empty.
	stepsToDoBeforeUpgrade := []func(context.Context, *testing.T, clusters.Cluster){
		func(ctx context.Context, t *testing.T, cluster clusters.Cluster) {
			t.Log("Applying Gateway API CRDs for v1.5.1 due to PR #3491")
			require.NoError(t, clusters.KustomizeDeployForCluster(ctx, cluster, "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v1.5.1"))
		},
	}
	assertionsAfterUpgrade := []assertion{
		{
			Name: "Gateway is programmed",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				gatewayAndItsListenersAreProgrammedAssertion("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
		{
			Name: "ControlPlane is ready",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				controlPlaneOwnedByGatewayReady("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
		{
			Name: "DataPlane is ready",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				dataPlaneOwnedByGatewayReady("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
		// Normally DataPlane Deployment should not be patched during the upgrade.
		{
			Name: "DataPlane deployment is patched after operator upgrade, due to PR #3531",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				gatewayDataPlaneDeploymentIsPatched("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
		{
			Name: "HTTPRoute responds with 200 status code and presents response header added by plugin",
			Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
				gatewayHTTPRoutingWorks("gateway-on-prem=true")(ctx, c, cl.MgrClient)
			},
		},
	}

	const releaseName = "ko-upgrade-test"
	helmOpts := &helm.Options{
		KubectlOptions: &k8s.KubectlOptions{
			Namespace:  e.Namespace.Name,
			RestConfig: e.Environment.Cluster().Config(),
		},
		SetValues: map[string]string{
			"readinessProbe.initialDelaySeconds": "1",
			"readinessProbe.periodSeconds":       "1",
			// Disable leader election and anonymous reports for tests.
			"env.no_leader_election": "true",
			"env.anonymous_reports":  "false",
		},
		Version: lastReleasedChartVersion,
		ExtraArgs: map[string][]string{
			"install": {
				"--devel",
				"--namespace", e.Namespace.Name,
			},
			"upgrade": {
				"--devel",
				"--namespace", e.Namespace.Name,
			},
			"uninstall": {
				"--wait",
				"--namespace", e.Namespace.Name,
			},
		},
	}

	t.Logf(
		"Installing Helm release %q with chart %q version %q",
		releaseName, lastReleasedChart, lastReleasedChartVersion,
	)
	require.NoError(t, helm.InstallE(t, helmOpts, lastReleasedChart, releaseName))
	out, err := helm.RunHelmCommandAndGetOutputE(t, helmOpts, "list")
	require.NoError(t, err)
	t.Logf("Helm list output after install:\n  %s", out)
	t.Cleanup(func() {
		out, err := helm.RunHelmCommandAndGetOutputE(t, helmOpts, "uninstall", releaseName)
		if !assert.NoError(t, err) {
			t.Logf("output: %s", out)
		}
	})
	ensureBasicReadiness(t, ctx, e, releaseName)

	require.NoError(
		t,
		waitForOperatorDeployment(
			t, ctx, e.Namespace.Name, e.Clients.K8sClient, waitTime, deploymentAssertConditions(t, deploymentReadyConditions()...),
		),
	)
	require.Eventually(
		t,
		waitForOperatorWebhookEventually(t, ctx, e.Namespace.Name, releaseName, e.Clients.K8sClient),
		webhookReadinessTimeout, webhookReadinessTick,
	)

	// Deploy the objects that should be present before the upgrade.
	cl := client.NewNamespacedClient(e.Clients.MgrClient, e.Namespace.Name)
	for _, obj := range objectsToDeploy {
		// NOTE: Create objects with eventually since we're deploying
		// admission webhook and that can take a moment to become ready.
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			obj := obj.DeepCopyObject().(client.Object)
			require.NoError(t, cl.Create(ctx, obj))
		}, waitTime, 500*time.Millisecond)
		t.Cleanup(func() {
			// Ensure that every object is properly deleted (the finalizer must
			// be executed, it requires some time) before the Helm chart is uninstalled.
			ctx, cancel := context.WithTimeout(context.Background(), waitTime)
			defer cancel()
			require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, obj)))
			eventually.WaitForObjectToNotExist(t, ctx, cl, obj, waitTime, time.Second)
		})
	}

	t.Logf("Checking assertions after install...")
	for _, assertion := range assertionsAfterInstall {
		t.Run("after_install/"+assertion.Name, func(t *testing.T) {
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				assertion.Func(c, e.Clients)
			}, waitTime, 500*time.Millisecond)
		})
	}

	if len(stepsToDoBeforeUpgrade) > 0 {
		t.Logf("Performing steps before upgrade...")
		for _, step := range stepsToDoBeforeUpgrade {
			step(ctx, t, e.Environment.Cluster())
		}
	}

	t.Logf(
		"Upgrading Helm release %q to chart %q with image %s:%s",
		releaseName, currentChart, currentImageRepository, currentImageTag,
	)
	helmOpts.SetValues["image.repository"] = currentImageRepository
	helmOpts.SetValues["image.tag"] = currentImageTag
	helmOpts.Version = "" // For local charts, version must be empty.
	require.NoError(t, helm.UpgradeE(t, helmOpts, currentChart, releaseName))

	out, err = helm.RunHelmCommandAndGetOutputE(t, helmOpts, "list")
	require.NoError(t, err)
	t.Logf("Helm list output after upgrade:\n  %s", out)
	ensureBasicReadiness(t, ctx, e, releaseName)

	t.Logf("Checking assertions after upgrade...")
	for _, assertion := range assertionsAfterUpgrade {
		t.Run("after_upgrade/"+assertion.Name, func(t *testing.T) {
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				assertion.Func(c, e.Clients)
			}, waitTime, 500*time.Millisecond)
		})
	}
}

func ensureBasicReadiness(
	t *testing.T, ctx context.Context, e TestEnvironment, releaseName string,
) {
	t.Helper()
	t.Log("ensure readiness of KO deployment and availability of webhook")
	require.NoError(
		t,
		waitForOperatorDeployment(
			t, ctx, e.Namespace.Name, e.Clients.K8sClient, waitTime, deploymentAssertConditions(t, deploymentReadyConditions()...),
		),
	)
	require.Eventually(
		t,
		waitForOperatorWebhookEventually(t, ctx, e.Namespace.Name, releaseName, e.Clients.K8sClient),
		webhookReadinessTimeout, webhookReadinessTick,
	)
}

func deploymentReadyConditions() []appsv1.DeploymentCondition {
	return []appsv1.DeploymentCondition{
		{
			Reason: "NewReplicaSetAvailable",
			Status: "True",
			Type:   "Progressing",
		},
		{
			Reason: "MinimumReplicasAvailable",
			Status: "True",
			Type:   "Available",
		},
	}
}

func splitRepoVersionFromImageOrFail(t *testing.T, image string) (string, string) {
	splitImage := strings.Split(image, ":")
	l := len(splitImage)
	if l < 2 {
		t.Fatalf("image %q does not contain a tag", image)
	}
	return strings.Join(splitImage[:l-1], ":"), splitImage[l-1]
}

// gatewayHTTPRoutingWorks verifies that HTTP requests to the Gateway's public IP
// are successfully routed to the backend through the HTTPRoute.
func gatewayHTTPRoutingWorks(gatewayLabelSelector string) func(ctx context.Context, c *assert.CollectT, cl client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		require.NotNil(c, gw, "Gateway not found with label selector %q", gatewayLabelSelector)

		require.NotEmpty(c, gw.Status.Addresses, "Gateway %q has no addresses in status", client.ObjectKeyFromObject(gw))
		gatewayIP := gw.Status.Addresses[0].Value
		require.NotEmpty(c, gatewayIP, "Gateway %q has empty address value", client.ObjectKeyFromObject(gw))

		// Make HTTP request to the Gateway's public IP at the /test path
		// which is the HTTPRoute path prefix
		url := fmt.Sprintf("http://%s:80/test", gatewayIP)
		httpClient := &http.Client{
			Timeout: 5 * time.Second,
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		require.NoError(c, err, "failed to create HTTP request to %q", url)

		require.EventuallyWithT(c, func(collect *assert.CollectT) {
			res, err := httpClient.Do(req)
			require.NoError(collect, err, "failed to make HTTP request to Gateway %q at %q", client.ObjectKeyFromObject(gw), url)
			defer res.Body.Close()
			require.Equal(collect, http.StatusOK, res.StatusCode,
				"expected HTTP 200 from Gateway %q at %q, got %d",
				client.ObjectKeyFromObject(gw), url, res.StatusCode,
			)
			require.Equal(
				collect, testHeaderValue, res.Header.Get(testHeaderKey),
				"expected response header %q to have value %q from Gateway %q at %q",
				testHeaderKey, testHeaderValue, client.ObjectKeyFromObject(gw), url,
			)
		}, waitTime, 1*time.Second)
	}
}

func getGatewayByLabelSelector(gatewayLabelSelector string, ctx context.Context, c *assert.CollectT, cl client.Client) *gatewayv1.Gateway {
	lReq, err := labels.ParseToRequirements(gatewayLabelSelector)
	require.NoError(c, err, "failed to parse label selector %q", gatewayLabelSelector)
	lSel := labels.NewSelector()
	for _, req := range lReq {
		lSel = lSel.Add(req)
	}

	var gws gatewayv1.GatewayList
	err = cl.List(ctx, &gws, &client.ListOptions{
		LabelSelector: lSel,
	})
	require.NoError(c, err, "failed to list gateways using label selector %q", gatewayLabelSelector)
	require.Len(c, gws.Items, 1, "expected exactly 1 Gateway with label selector %q", gatewayLabelSelector)

	return &gws.Items[0]
}

// gatewayAndItsListenersAreProgrammedAssertion returns a predicate that checks
// if the Gateway and its listeners are programmed.
func gatewayAndItsListenersAreProgrammedAssertion(gatewayLabelSelector string) func(context.Context, *assert.CollectT, client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		require.NotNil(c, gw, "Gateway not found with label selector %q", gatewayLabelSelector)

		require.True(c, gateway.IsProgrammed(gw), "Gateway %q is not programmed: %s", client.ObjectKeyFromObject(gw), pretty.Sprint(gw))
		require.True(c, gateway.AreListenersProgrammed(gw), "Listeners of Gateway %q are not programmed: %s", client.ObjectKeyFromObject(gw), pretty.Sprint(gw))
	}
}

func gatewayDataPlaneDeploymentIsNotPatched(
	gatewayLabelSelector string,
) func(context.Context, *assert.CollectT, client.Client) {
	return gatewayDataPlaneDeploymentCheck(gatewayLabelSelector, func(d *appsv1.Deployment) error {
		if d.Generation != 1 {
			return fmt.Errorf("Gateway's DataPlane Deployment %q got patched but it shouldn't:\n%# v",
				client.ObjectKeyFromObject(d), pretty.Formatter(d),
			)
		}
		return nil
	})
}

func gatewayDataPlaneDeploymentIsPatched(
	gatewayLabelSelector string,
) func(context.Context, *assert.CollectT, client.Client) {
	return gatewayDataPlaneDeploymentCheck(gatewayLabelSelector, func(d *appsv1.Deployment) error {
		if d.Generation == 1 {
			return fmt.Errorf("Gateway's DataPlane Deployment %q did not get patched but it should:\n%# v",
				client.ObjectKeyFromObject(d), pretty.Formatter(d),
			)
		}
		return nil
	})
}

func gatewayDataPlaneDeploymentCheck(
	gatewayLabelSelector string,
	predicates ...func(d *appsv1.Deployment) error,
) func(context.Context, *assert.CollectT, client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		require.NotNil(c, gw, "Gateway not found with label selector %q", gatewayLabelSelector)

		deployments, err := listDataPlaneDeploymentsForGateway(c, ctx, cl, gw)
		require.NoError(c, err, "failed to list DataPlane Deployments for Gateway %q", client.ObjectKeyFromObject(gw))
		require.Len(
			c, deployments, 1, "expected 1 DataPlane Deployment for Gateway %q, got %d", client.ObjectKeyFromObject(gw), len(deployments),
		)

		deployment := &deployments[0]
		for _, predicate := range predicates {
			require.NoError(c, predicate(deployment))
		}
	}
}

func listDataPlaneDeploymentsForGateway(
	c *assert.CollectT,
	ctx context.Context,
	cl client.Client,
	gw *gatewayv1.Gateway,
) ([]appsv1.Deployment, error) {
	dataPlanes, err := gateway.ListDataPlanesForGateway(ctx, cl, gw)
	require.NoError(c, err, "failed to list DataPlanes for Gateway %q: %v", client.ObjectKeyFromObject(gw), err)
	require.Len(c, dataPlanes, 1, "expected 1 DataPlane for Gateway %q, got %d", client.ObjectKeyFromObject(gw), len(dataPlanes))

	dataPlane := &dataPlanes[0]
	return k8sutils.ListDeploymentsForOwner(
		ctx,
		cl,
		dataPlane.Namespace,
		dataPlane.UID,
		client.MatchingLabels{
			"app": dataPlane.Name,
		},
	)
}

// controlPlaneOwnedByGatewayReady is the predicate that asserts the ControlPlane owned by the gateway is ready.
func controlPlaneOwnedByGatewayReady(gatewayLabelSelector string) func(ctx context.Context, c *assert.CollectT, cl client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		require.NotNil(c, gw, "Gateway not found with label selector %q", gatewayLabelSelector)

		controlPlanes, err := gateway.ListControlPlanesForGateway(ctx, cl, gw)
		require.NoError(c, err, "failed to list ControlPlanes for Gateway %q: %v", client.ObjectKeyFromObject(gw), err)
		require.Len(c, controlPlanes, 1, "expected 1 ControlPlane for Gateway %q, got %d", client.ObjectKeyFromObject(gw), len(controlPlanes))
		cp := controlPlanes[0]

		ready := lo.ContainsBy(cp.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == "Ready" && condition.Status == metav1.ConditionTrue
		})
		require.True(c, ready, "ControlPlane is not ready")
	}
}

// dataPlaneOwnedByGatewayReady is the predicate that asserts the DataPlane owned by the gateway is ready.
func dataPlaneOwnedByGatewayReady(gatewayLabelSelector string) func(ctx context.Context, c *assert.CollectT, cl client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		require.NotNil(c, gw, "Gateway not found with label selector %q", gatewayLabelSelector)

		dataPlanes, err := gateway.ListDataPlanesForGateway(ctx, cl, gw)
		require.NoError(c, err, "failed to list DataPlanes for Gateway %q: %v", client.ObjectKeyFromObject(gw), err)
		require.Len(c, dataPlanes, 1, "expected 1 DataPlane for Gateway %q, got %d", client.ObjectKeyFromObject(gw), len(dataPlanes))
		dp := dataPlanes[0]

		ready := lo.ContainsBy(dp.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == "Ready" && condition.Status == metav1.ConditionTrue
		})
		require.True(c, ready, "DataPlane is not ready")
	}
}
