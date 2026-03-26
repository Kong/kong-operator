package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/kr/pretty"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"

	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

const testHeaderKey = "Header-Added-By-Plugin"

type gatewayMode string

const (
	gatewayModeOnPrem gatewayMode = "on-prem"
	gatewayModeHybrid gatewayMode = "hybrid"
)

type certBootstrapMode string

const (
	certManager certBootstrapMode = "cert-manager"
	chart       certBootstrapMode = "chart"
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

	for _, certMode := range []certBootstrapMode{certManager, chart} {
		t.Run(fmt.Sprintf("certificates from %s", certMode), func(t *testing.T) {

			// CreateEnvironment will queue up environment cleanup if necessary
			// and dumping diagnostics if the test fails.
			e := CreateEnvironment(t, ctx)

			// Assertion is run after the upgrade to assert the state of the resources in the cluster.
			type assertion struct {
				Name string
				Func func(*assert.CollectT, *testutils.K8sClients)
			}
			type suite struct {
				Name                   string
				Objects                []client.Object
				AssertionsAfterInstall []assertion
				AssertionsAfterUpgrade []assertion
			}

			// This is the place to add steps that should be performed before the upgrade.
			// For instance change in CRDs requires manual installation of new CRDs before the upgrade,
			// see charts/kong-operator/UPGRADE.md this section mostly should be empty.
			// IDEALLY IT SHOULD BE EMPTY - seamless upgrade should not require manual steps.
			stepsToDoBeforeUpgrade := []func(context.Context, *testing.T, clusters.Cluster){
				func(ctx context.Context, t *testing.T, cluster clusters.Cluster) {
					t.Log("Applying Gateway API CRDs for v1.5.1 due to PR #3491")
					require.NoError(t, clusters.KustomizeDeployForCluster(ctx, cluster, "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v1.5.1"))
				},
			}

			onPremObjects, onPremGatewayLabelSelector := objectsToDeployForMode(t, e, gatewayModeOnPrem)
			suitesToRun := []suite{
				{
					Name:    "on-prem",
					Objects: onPremObjects,
					AssertionsAfterInstall: []assertion{
						{
							Name: "Gateway is programmed",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayAndItsListenersAreProgrammedAssertion(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "ControlPlane is ready",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								controlPlaneOwnedByGatewayReady(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "DataPlane is ready",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								dataPlaneOwnedByGatewayReady(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},

						{
							Name: "DataPlane deployment is not patched after install",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayDataPlaneDeploymentIsNotPatched(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "HTTPRoute responds with 200 status code and presents response header added by plugin",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayHTTPRoutingWorks(onPremGatewayLabelSelector, gatewayModeOnPrem)(ctx, c, cl.MgrClient)
							},
						},
					},
					AssertionsAfterUpgrade: []assertion{
						{
							Name: "Gateway is programmed",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayAndItsListenersAreProgrammedAssertion(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "ControlPlane is ready",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								controlPlaneOwnedByGatewayReady(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "DataPlane is ready",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								dataPlaneOwnedByGatewayReady(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						// Normally DataPlane Deployment should not be patched during the upgrade.
						{
							Name: "DataPlane deployment is patched after operator upgrade, due to PR #3531",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayDataPlaneDeploymentIsPatched(onPremGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "HTTPRoute responds with 200 status code and presents response header added by plugin",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayHTTPRoutingWorks(onPremGatewayLabelSelector, gatewayModeOnPrem)(ctx, c, cl.MgrClient)
							},
						},
					},
				},
			}

			if testenv.KonnectAccessToken() != "" && testenv.KonnectServerURL() != "" {
				hybridObjects, hybridGatewayLabelSelector := objectsToDeployForMode(t, e, gatewayModeHybrid)
				suitesToRun = append(suitesToRun, suite{
					Name:    "hybrid",
					Objects: hybridObjects,
					AssertionsAfterInstall: []assertion{
						{
							Name: "Gateway is programmed",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayAndItsListenersAreProgrammedAssertion(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "KonnectGatewayControlPlane is programmed",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								konnectGatewayControlPlaneOwnedByGatewayProgrammed(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "DataPlane is ready",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								dataPlaneOwnedByGatewayReady(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "DataPlane deployment is not patched after install",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayDataPlaneDeploymentIsNotPatched(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "HTTPRoute responds with 200 status code and presents response header added by plugin",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayHTTPRoutingWorks(hybridGatewayLabelSelector, gatewayModeHybrid)(ctx, c, cl.MgrClient)
							},
						},
					},
					AssertionsAfterUpgrade: []assertion{
						{
							Name: "Gateway is programmed",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayAndItsListenersAreProgrammedAssertion(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "KonnectGatewayControlPlane is programmed",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								konnectGatewayControlPlaneOwnedByGatewayProgrammed(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "DataPlane is ready",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								dataPlaneOwnedByGatewayReady(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "DataPlane deployment is patched after operator upgrade, due to PR #3531",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayDataPlaneDeploymentIsPatched(hybridGatewayLabelSelector)(ctx, c, cl.MgrClient)
							},
						},
						{
							Name: "HTTPRoute responds with 200 status code and presents response header added by plugin",
							Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
								gatewayHTTPRoutingWorks(hybridGatewayLabelSelector, gatewayModeHybrid)(ctx, c, cl.MgrClient)
							},
						},
					},
				})
			} else {
				t.Log(
					"Skipping tests for Hybrid Gateway, KONG_TEST_KONNECT_ACCESS_TOKEN and/or KONG_TEST_KONNECT_SERVER_URL env vars are not set",
				)
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
					"env.enable_controller_konnect":      "true",
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
			if certMode == certManager {
				helmOpts.SetValues["global.webhooks.options.certManager.enabled"] = "true"
				helmOpts.SetValues["global.certificateAuthority.options.certManager.enabled"] = "true"
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

			// Deploy the objects that should be present before the upgrade.
			cl := client.NewNamespacedClient(e.Clients.MgrClient, e.Namespace.Name)
			for _, suite := range suitesToRun {
				t.Logf("Deploying objects for suite %q...", suite.Name)
				for _, obj := range suite.Objects {
					obj := obj.DeepCopyObject().(client.Object)
					require.NoError(t, cl.Create(ctx, obj))
					t.Cleanup(func() {
						// Ensure that every object is properly deleted (the finalizer must
						// be executed, it requires some time) before the Helm chart is uninstalled.
						ctx, cancel := context.WithTimeout(context.Background(), waitTime)
						defer cancel()
						require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, obj)))
						eventually.WaitForObjectToNotExist(t, ctx, cl, obj, waitTime, time.Second)
					})
				}
			}

			t.Logf("Checking assertions after install...")
			for _, suite := range suitesToRun {
				for _, assertion := range suite.AssertionsAfterInstall {
					t.Run(suite.Name+"/after_install/"+assertion.Name, func(t *testing.T) {
						require.EventuallyWithT(t, func(c *assert.CollectT) {
							assertion.Func(c, e.Clients)
						}, waitTime, 500*time.Millisecond)
					})
				}
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
			for _, suite := range suitesToRun {
				t.Logf("Running assertions for suite %q...", suite.Name)
				for _, assertion := range suite.AssertionsAfterUpgrade {
					t.Run(suite.Name+"/after_upgrade/"+assertion.Name, func(t *testing.T) {
						require.EventuallyWithT(t, func(c *assert.CollectT) {
							assertion.Func(c, e.Clients)
						}, waitTime, 500*time.Millisecond)
					})
				}
			}
		})
	}
}

func objectsToDeployForMode(
	t *testing.T,
	e TestEnvironment,
	gatewayMode gatewayMode,
) ([]client.Object, string) {
	t.Helper()

	const workloadLabelKey = "gateway-under-test"

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
		types.NamespacedName{Namespace: e.Namespace.Name, Name: fmt.Sprintf("gateway-%s", gatewayMode)},
		gatewayClass,
		func(gw *gatewayv1.Gateway) {
			gw.Labels = map[string]string{workloadLabelKey: string(gatewayMode)}
		},
	)
	// Create a test service backend for the HTTPRoute.
	container := generators.NewContainer(fmt.Sprintf("httpbin-%s", gatewayMode), testutils.HTTPBinImage, 80)
	testDeployment := generators.NewDeploymentForContainer(container)
	testDeployment.Namespace = e.Namespace.Name
	testService := generators.NewServiceForDeployment(testDeployment, corev1.ServiceTypeClusterIP)
	testService.Namespace = e.Namespace.Name
	kongPlugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: e.Namespace.Name,
			Name:      fmt.Sprintf("response-transformer-add-header-%s", gatewayMode),
		},
		PluginName: "response-transformer",
		Config: apiextensionsv1.JSON{
			Raw: fmt.Appendf(nil, `{"add":{"headers":["%s:%s"]}}`, testHeaderKey, gatewayMode),
		},
	}
	httpRoute := helpers.GenerateHTTPRoute(
		e.Namespace.Name, gateway.Name, testService.Name, func(h *gatewayv1.HTTPRoute) {
			// For on-prem it's typical to attach plugin with annotations.
			// For Hybrid Gateway annotation is not supported by choice, hence use
			// ExtensionRef to reference the plugin.
			switch gatewayMode {
			case gatewayModeOnPrem:
				h.Annotations["konghq.com/plugins"] = kongPlugin.Name
			case gatewayModeHybrid:
				h.Spec.Rules[0].Filters = []gatewayv1.HTTPRouteFilter{
					{
						Type: gatewayv1.HTTPRouteFilterExtensionRef,
						ExtensionRef: &gatewayv1.LocalObjectReference{
							Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
							Kind:  "KongPlugin",
							Name:  gatewayv1.ObjectName(kongPlugin.Name),
						},
					},
				}
			}
		},
	)

	objects := []client.Object{
		gatewayConfig,
		gatewayClass,
		gateway,
		testDeployment,
		testService,
		kongPlugin,
		httpRoute,
	}

	if gatewayMode == gatewayModeHybrid {
		konnectAccessToken := testenv.KonnectAccessToken()
		konnectServerURL := testenv.KonnectServerURL()
		require.NotEmpty(t, konnectAccessToken, "hybrid deployment mode requires KONG_TEST_KONNECT_ACCESS_TOKEN")
		require.NotEmpty(t, konnectServerURL, "hybrid deployment mode requires KONG_TEST_KONNECT_SERVER_URL")

		konnectAPIAuthConfiguration := &konnectv1alpha1.KonnectAPIAuthConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: e.Namespace.Name,
				Name:      fmt.Sprintf("api-auth-config-%s", gatewayMode),
			},
			Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
				Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
				Token:     konnectAccessToken,
				ServerURL: konnectServerURL,
			},
		}

		gatewayConfig.Spec.Konnect = &operatorv2beta1.KonnectOptions{
			APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
				Name: konnectAPIAuthConfiguration.Name,
			},
		}

		objects = append([]client.Object{konnectAPIAuthConfiguration}, objects...)
	}

	return objects, fmt.Sprintf("%s=%s", workloadLabelKey, gatewayMode)
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

func splitRepoVersionFromImage(t *testing.T, image string) (string, string) {
	splitImage := strings.Split(image, ":")
	l := len(splitImage)
	if l < 2 {
		t.Fatalf("image %q does not contain a tag", image)
	}
	return strings.Join(splitImage[:l-1], ":"), splitImage[l-1]
}

func baseGatewayConfigurationSpec() operatorv2beta1.GatewayConfigurationSpec {
	return operatorv2beta1.GatewayConfigurationSpec{
		DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
			Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
				DeploymentOptions: operatorv2beta1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: consts.DataPlaneProxyContainerName,
									ReadinessProbe: &corev1.Probe{
										InitialDelaySeconds: 1,
										PeriodSeconds:       1,
									},
								},
							},
						},
					},
				},
			},
		},

		// TODO(pmalek): add support for ControlPlane optionns using GatewayConfiguration v2
		// https://github.com/kong/kong-operator/issues/1728
	}
}

func getGatewayByLabelSelector(gatewayLabelSelector string, ctx context.Context, c *assert.CollectT, cl client.Client) *gatewayv1.Gateway {
	lReq, err := labels.ParseToRequirements(gatewayLabelSelector)
	if err != nil {
		c.Errorf("failed to parse label selector %q: %v", gatewayLabelSelector, err)
		return nil
	}
	lSel := labels.NewSelector()
	for _, req := range lReq {
		lSel = lSel.Add(req)
	}

	var gws gatewayv1.GatewayList
	err = cl.List(ctx, &gws, &client.ListOptions{
		LabelSelector: lSel,
	})
	if err != nil {
		c.Errorf("failed to list gateways using label selector %q: %v", gatewayLabelSelector, err)
		return nil
	}

	if len(gws.Items) != 1 {
		c.Errorf("expected 1 Gateway, got %d", len(gws.Items))
		return nil
	}

	return &gws.Items[0]
}

// gatewayAndItsListenersAreProgrammedAssertion returns a predicate that checks
// if the Gateway and its listeners are programmed.
func gatewayAndItsListenersAreProgrammedAssertion(gatewayLabelSelector string) func(context.Context, *assert.CollectT, client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		if !assert.NotNil(c, gw) {
			return
		}
		assert.True(c, gateway.IsProgrammed(gw), "Gateway %q is not programmed: %s", client.ObjectKeyFromObject(gw), pretty.Sprint(gw))
		assert.True(c, gateway.AreListenersProgrammed(gw), "Listeners of Gateway %q are not programmed: %s", client.ObjectKeyFromObject(gw), pretty.Sprint(gw))
	}
}

func gatewayDataPlaneDeploymentHasImageSetTo(
	gatewayLabelSelector string,
	image string,
) func(context.Context, *assert.CollectT, client.Client) {
	return gatewayDataPlaneDeploymentCheck(gatewayLabelSelector, func(d *appsv1.Deployment) error {
		container := d.Spec.Template.Spec.Containers
		if len(container) != 1 {
			return fmt.Errorf("expected 1 container in Deployment %q, got %d",
				client.ObjectKeyFromObject(d), len(d.Spec.Template.Spec.Containers),
			)
		}

		if container[0].Image != image {
			return fmt.Errorf("Gateway's DataPlane Deployment %q expected image %s got %s",
				client.ObjectKeyFromObject(d), image, container[0].Image,
			)
		}
		return nil
	})
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
		if !assert.NotNil(c, gw) {
			return
		}
		deployments, err := listDataPlaneDeploymentsForGateway(c, ctx, cl, gw)
		if err != nil {
			return
		}

		if !assert.Len(c, deployments, 1) {
			c.Errorf("expected 1 DataPlane Deployment for Gateway %q, got %d", client.ObjectKeyFromObject(gw), len(deployments))
			return
		}

		deployment := &deployments[0]
		for _, predicate := range predicates {
			assert.NoError(c, predicate(deployment))
		}
	}
}

func listDataPlaneDeploymentsForGateway(
	c *assert.CollectT,
	ctx context.Context,
	cl client.Client,
	gw *gatewayv1.Gateway,
) ([]appsv1.Deployment, error) {
	dataplanes, err := gateway.ListDataPlanesForGateway(ctx, cl, gw)
	if err != nil {
		return nil, fmt.Errorf("failed to list DataPlanes for Gateway %q: %w", client.ObjectKeyFromObject(gw), err)
	}
	if !assert.Len(c, dataplanes, 1) {
		return nil, fmt.Errorf("expected 1 DataPlane for Gateway %q, got %d", client.ObjectKeyFromObject(gw), len(dataplanes))
	}

	dataplane := &dataplanes[0]
	return k8sutils.ListDeploymentsForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			"app": dataplane.Name,
		},
	)
}

// controlPlaneOwnedByGatewayReady is the predicate that asserts the ControlPlane owned by the gateway is ready.
func controlPlaneOwnedByGatewayReady(gatewayLabelSelector string) func(ctx context.Context, c *assert.CollectT, cl client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		if !assert.NotNil(c, gw) {
			return
		}

		controlplanes, err := gateway.ListControlPlanesForGateway(ctx, cl, gw)
		if err != nil {
			c.Errorf("failed to list ControlPlanes for Gateway %q: %v", client.ObjectKeyFromObject(gw), err)
			return
		}
		if !assert.Len(c, controlplanes, 1) {
			return
		}
		cp := controlplanes[0]

		if ready := lo.ContainsBy(cp.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == "Ready" && condition.Status == metav1.ConditionTrue
		}); !assert.True(c, ready, "ControlPlane is not ready") {
			return
		}
	}
}
