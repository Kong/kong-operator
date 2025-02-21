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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
	"github.com/kong/gateway-operator/test/helpers"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func init() {
	addTestsToTestSuite(TestHelmUpgrade)
}

func TestHelmUpgrade(t *testing.T) {
	const (
		// Rel: https://github.com/Kong/charts/tree/main/charts/gateway-operator
		chart = "kong/gateway-operator"

		waitTime = 3 * time.Minute
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// createEnvironment will queue up environment cleanup if necessary
	// and dumping diagnostics if the test fails.
	e := CreateEnvironment(t, ctx)

	// Assertion is run after the upgrade to assert the state of the resources in the cluster.
	type assertion struct {
		Name string
		Func func(*assert.CollectT, *testutils.K8sClients)
	}

	testCases := []struct {
		name             string
		fromVersion      string
		toVersion        string
		objectsToDeploy  []client.Object
		upgradeToCurrent bool
		// If upgrading to an image tag that's not a valid semver, fill this to the effective semver so that charts
		// can correctly render semver-conditional templates.
		upgradeToEffectiveSemver string
		assertionsAfterInstall   []assertion
		assertionsAfterUpgrade   []assertion
	}{
		{
			name:        "upgrade from one before latest to latest minor",
			fromVersion: "1.3.0", // renovate: datasource=docker packageName=kong/gateway-operator-oss depName=kong/gateway-operator-oss@only-patch
			toVersion:   "1.4.0", // renovate: datasource=docker packageName=kong/gateway-operator-oss
			objectsToDeploy: []client.Object{
				&operatorv1beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwconf-upgrade-onebeforelatestminor-latestminor",
					},
					Spec: baseGatewayConfigurationSpec(),
				},
				&gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwclass-upgrade-onebeforelatestminor-latestminor",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ParametersRef: &gatewayv1.ParametersReference{
							Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
							Kind:      gatewayv1.Kind("GatewayConfiguration"),
							Namespace: (*gatewayv1.Namespace)(&e.Namespace.Name),
							Name:      "gwconf-upgrade-onebeforelatestminor-latestminor",
						},
						ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
					},
				},
				&gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "gw-upgrade-onebeforelatestminor-latestminor-",
						Labels: map[string]string{
							"gw-upgrade-onebeforelatestminor-latestminor": "true",
						},
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName("gwclass-upgrade-onebeforelatestminor-latestminor"),
						Listeners: []gatewayv1.Listener{{
							Name:     "http",
							Protocol: gatewayv1.HTTPProtocolType,
							Port:     gatewayv1.PortNumber(80),
						}},
					},
				},
			},
			assertionsAfterInstall: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-onebeforelatestminor-latestminor=true")(ctx, c, cl.MgrClient)
					},
				},
			},
			assertionsAfterUpgrade: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-onebeforelatestminor-latestminor=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "DataPlane deployment is patched after operator upgrade (due to change in default Kong image version to 3.8)",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayDataPlaneDeploymentIsPatched("gw-upgrade-onebeforelatestminor-latestminor=true")(ctx, c, cl.MgrClient)
						gatewayDataPlaneDeploymentHasImageSetTo("gw-upgrade-onebeforelatestminor-latestminor=true", helpers.GetDefaultDataPlaneBaseImage()+":3.8")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "Cluster wide resources owned by the ControlPlane get the proper set of labels",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						clusterWideResourcesAreProperlyManaged("gw-upgrade-onebeforelatestminor-latestminor=true")(ctx, c, cl.MgrClient)
					},
				},
			},
		},
		{
			name:             "upgrade from latest minor to current",
			fromVersion:      "1.4.0", // renovate: datasource=docker packageName=kong/gateway-operator-oss
			upgradeToCurrent: true,
			// This is the effective semver of a next release. It's needed for the chart to properly render
			// semver-conditional templates.
			upgradeToEffectiveSemver: "1.5.0",
			objectsToDeploy: []client.Object{
				&operatorv1beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwconf-upgrade-latestminor-current",
					},
					Spec: baseGatewayConfigurationSpec(),
				},
				&gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwclass-upgrade-latestminor-current",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ParametersRef: &gatewayv1.ParametersReference{
							Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
							Kind:      gatewayv1.Kind("GatewayConfiguration"),
							Namespace: (*gatewayv1.Namespace)(&e.Namespace.Name),
							Name:      "gwconf-upgrade-latestminor-current",
						},
						ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
					},
				},
				&gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "gw-upgrade-latestminor-current-",
						Labels: map[string]string{
							"gw-upgrade-latestminor-current": "true",
						},
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName("gwclass-upgrade-latestminor-current"),
						Listeners: []gatewayv1.Listener{{
							Name:     "http",
							Protocol: gatewayv1.HTTPProtocolType,
							Port:     gatewayv1.PortNumber(80),
						}},
					},
				},
			},
			assertionsAfterInstall: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-latestminor-current=true")(ctx, c, cl.MgrClient)
					},
				},
			},
			assertionsAfterUpgrade: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-latestminor-current=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "DataPlane deployment is not patched after operator upgrade",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayDataPlaneDeploymentIsPatched("gw-upgrade-latestminor-current=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "Cluster wide resources owned by the ControlPlane get the proper set of labels",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						clusterWideResourcesAreProperlyManaged("gw-upgrade-latestminor-current=true")(ctx, c, cl.MgrClient)
					},
				},
			},
		},
		/**
		// TODO(Jintao): This test is disabled. After a new nightly image is available which uses KIC 3.4.1, we can enable it.
		{
			name:             "upgrade from nightly to current",
			fromVersion:      "nightly",
			upgradeToCurrent: true,
			objectsToDeploy: []client.Object{
				&operatorv1beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwconf-upgrade-nightly-current",
					},
					Spec: baseGatewayConfigurationSpec(),
				},
				&gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwclass-upgrade-nightly-to-current",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ParametersRef: &gatewayv1.ParametersReference{
							Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
							Kind:      gatewayv1.Kind("GatewayConfiguration"),
							Namespace: (*gatewayv1.Namespace)(&e.Namespace.Name),
							Name:      "gwconf-upgrade-nightly-current",
						},
						ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
					},
				},
				&gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "gw-upgrade-nightly-to-current-",
						Labels: map[string]string{
							"gw-upgrade-nightly-to-current": "true",
						},
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName("gwclass-upgrade-nightly-to-current"),
						Listeners: []gatewayv1.Listener{{
							Name:     "http",
							Protocol: gatewayv1.HTTPProtocolType,
							Port:     gatewayv1.PortNumber(80),
						}},
					},
				},
			},
			assertionsAfterInstall: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-nightly-to-current=true")(ctx, c, cl.MgrClient)
					},
				},
			},
			assertionsAfterUpgrade: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-nightly-to-current=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "DataPlane deployment is not patched after operator upgrade",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayDataPlaneDeploymentIsPatched("gw-upgrade-nightly-to-current=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "Cluster wide resources owned by the ControlPlane get the proper set of labels",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						clusterWideResourcesAreProperlyManaged("gw-upgrade-nightly-to-current=true")(ctx, c, cl.MgrClient)
					},
				},
			},
		},
		**/
	}

	var (
		currentRepository string
		currentTag        string
	)
	if imageLoad != "" {
		t.Logf("KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD set to %q", imageLoad)
		currentRepository, currentTag = splitRepoVersionFromImage(t, imageLoad)
	} else if imageOverride != "" {
		t.Logf("KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE set to %q", imageOverride)
		currentRepository, currentTag = splitRepoVersionFromImage(t, imageOverride)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Repository is different for OSS and Enterprise images and it should be set accordingly.
			kgoImageRepository := "docker.io/kong/gateway-operator-oss"
			if helpers.GetDefaultDataPlaneBaseImage() == consts.DefaultDataPlaneBaseEnterpriseImage {
				kgoImageRepository = "docker.io/kong/gateway-operator"
			}
			var (
				tag              string
				targetRepository = kgoImageRepository
			)
			if tc.upgradeToCurrent {
				if currentTag == "" {
					t.Skip(
						"No KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE nor KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD env specified. " +
							"Please specify the image to upgrade to in order to run this test.",
					)
				}
				tag = currentTag
				targetRepository = currentRepository
			} else {
				tag = tc.toVersion
			}

			tagInReleaseName := tag
			if len(tag) > 8 {
				tagInReleaseName = tag[:8]
			}
			releaseName := strings.ReplaceAll(fmt.Sprintf("kgo-%s-to-%s", tc.fromVersion, tagInReleaseName), ".", "-")
			values := map[string]string{
				"image.tag":                          tc.fromVersion,
				"image.repository":                   kgoImageRepository,
				"readinessProbe.initialDelaySeconds": "1",
				"readinessProbe.periodSeconds":       "1",
				// Disable leader election and anonymous reports for tests.
				"no_leader_election": "true",
				"anonymous_reports":  "false",
			}

			if tc.upgradeToEffectiveSemver != "" {
				values["image.effectiveSemver"] = tc.upgradeToEffectiveSemver
			}

			opts := &helm.Options{
				KubectlOptions: &k8s.KubectlOptions{
					Namespace:  e.Namespace.Name,
					RestConfig: e.Environment.Cluster().Config(),
				},
				SetValues: values,
			}

			require.NoError(t, helm.AddRepoE(t, opts, "kong", "https://charts.konghq.com"))
			require.NoError(t, helm.InstallE(t, opts, chart, releaseName))
			t.Cleanup(func() {
				out, err := helm.RunHelmCommandAndGetOutputE(t, opts, "uninstall", releaseName)
				if !assert.NoError(t, err) {
					t.Logf("output: %s", out)
				}
			})

			require.NoError(t, waitForOperatorDeployment(t, ctx, e.Namespace.Name, e.Clients.K8sClient, waitTime,
				deploymentAssertConditions(t, deploymentReadyConditions()...),
			))

			// Deploy the objects that should be present before the upgrade.
			cl := client.NewNamespacedClient(e.Clients.MgrClient, e.Namespace.Name)
			for _, obj := range tc.objectsToDeploy {
				require.NoError(t, cl.Create(ctx, obj))
				t.Cleanup(func() {
					require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, obj)))
				})
			}

			t.Logf("Checking assertions after install...")
			for _, assertion := range tc.assertionsAfterInstall {
				t.Run("after_install/"+assertion.Name, func(t *testing.T) {
					require.EventuallyWithT(t, func(c *assert.CollectT) {
						assertion.Func(c, e.Clients)
					}, waitTime, 500*time.Millisecond)
				})
			}

			t.Logf("Upgrading from %s to %s", tc.fromVersion, tag)
			opts.SetValues["image.tag"] = tag
			opts.SetValues["image.repository"] = targetRepository

			require.NoError(t, helm.UpgradeE(t, opts, chart, releaseName))
			require.NoError(t, waitForOperatorDeployment(t, ctx, e.Namespace.Name, e.Clients.K8sClient, waitTime,
				deploymentAssertConditions(t, deploymentReadyConditions()...),
			),
			)

			t.Logf("Checking assertions after upgrade...")
			for _, assertion := range tc.assertionsAfterUpgrade {
				t.Run("after_upgrade/"+assertion.Name, func(t *testing.T) {
					require.EventuallyWithT(t, func(c *assert.CollectT) {
						assertion.Func(c, e.Clients)
					}, waitTime, 500*time.Millisecond)
				})
			}
		})
	}
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

func baseGatewayConfigurationSpec() operatorv1beta1.GatewayConfigurationSpec {
	return operatorv1beta1.GatewayConfigurationSpec{
		DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
			Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
				DeploymentOptions: operatorv1beta1.DeploymentOptions{
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

		ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
			Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: consts.ControlPlaneControllerContainerName,
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

func gatewayDataPlaneDeploymentIsNotPatched( //nolint:unused
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

func clusterWideResourcesAreProperlyManaged(gatewayLabelSelector string) func(ctx context.Context, c *assert.CollectT, cl client.Client) {
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
		cp := &controlplanes[0]

		managedByLabelSet := k8sutils.GetManagedByLabelSet(cp)

		clusterRoles, err := k8sutils.ListClusterRoles(
			ctx,
			cl,
			client.MatchingLabels(managedByLabelSet),
		)
		assert.NoError(c, err)
		assert.Len(c, clusterRoles, 1)

		clusterRoleBindings, err := k8sutils.ListClusterRoleBindings(
			ctx,
			cl,
			client.MatchingLabels(managedByLabelSet),
		)
		assert.NoError(c, err)
		assert.Len(c, clusterRoleBindings, 1)

		validatingWebhookConfigurations, err := k8sutils.ListValidatingWebhookConfigurations(
			ctx,
			cl,
			client.MatchingLabels(managedByLabelSet),
		)
		assert.NoError(c, err)
		assert.Len(c, validatingWebhookConfigurations, 1)
	}
}
