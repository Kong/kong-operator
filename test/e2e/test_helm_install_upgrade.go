package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
	"github.com/kong/gateway-operator/test/helpers"
)

func init() {
	addTestsToTestSuite(TestHelmUpgrade)
}

func TestHelmUpgrade(t *testing.T) {
	const (
		// Rel: https://github.com/Kong/charts/tree/main/charts/gateway-operator
		chart = "kong/gateway-operator"
		image = "docker.io/kong/gateway-operator-oss"

		waitTime = 3 * time.Minute
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// createEnvironment will queue up environment cleanup if necessary
	// and dumping diagnostics if the test fails.
	e := CreateEnvironment(t, ctx)

	// assertion is run after the upgrade to assert the state of the resources in the cluster.
	type assertion struct {
		Name string
		Func func(*assert.CollectT, *testutils.K8sClients)
	}

	testcases := []struct {
		name                   string
		fromVersion            string
		toVersion              string
		objectsToDeploy        []client.Object
		upgradeToCurrent       bool
		assertionsAfterUpgrade []assertion
	}{
		// NOTE: We do not support versions earlier than 1.2 with the helm chart.
		// The initial version of the chart contained CRDs from KGO 1.2. which
		// introduced a breaking change which makes it impossible to upgrade from
		// automatically (without manually deleting the CRDs).
		{
			name:        "upgrade from 1.2.0 to 1.2.3",
			fromVersion: "1.2.0",
			toVersion:   "1.2.3",
			objectsToDeploy: []client.Object{
				&operatorv1beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwconf-upgrade-120-123",
					},
					Spec: baseGatewayConfigurationSpec(),
				},
				&gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwclass-upgrade-120-123",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ParametersRef: &gatewayv1.ParametersReference{
							Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
							Kind:      gatewayv1.Kind("GatewayConfiguration"),
							Namespace: (*gatewayv1.Namespace)(&e.Namespace.Name),
							Name:      "gwconf-upgrade-120-123",
						},
						ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
					},
				},
				&gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "gw-upgrade-120-123-",
						Labels: map[string]string{
							"gw-upgrade-120-123": "true",
						},
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName("gwclass-upgrade-120-123"),
						Listeners: []gatewayv1.Listener{{
							Name:     "http",
							Protocol: gatewayv1.HTTPProtocolType,
							Port:     gatewayv1.PortNumber(80),
						}},
					},
				},
			},
			assertionsAfterUpgrade: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-120-123=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "DataPlane deployment is not patched after operator upgrade",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayDataPlaneDeploymentIsNotPatched("gw-upgrade-120-123=true")(ctx, c, cl.MgrClient)
					},
				},
			},
		},
		{
			// TODO: use renovate to bump the version in these 2 lines.
			// https://github.com/Kong/gateway-operator/issues/121
			name:             "upgrade from 1.2.3 to current",
			fromVersion:      "1.2.3",
			upgradeToCurrent: true,
			objectsToDeploy: []client.Object{
				&operatorv1beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwconf-upgrade-123-current",
					},
					Spec: baseGatewayConfigurationSpec(),
				},
				&gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gwclass-upgrade-123-current",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ParametersRef: &gatewayv1.ParametersReference{
							Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
							Kind:      gatewayv1.Kind("GatewayConfiguration"),
							Namespace: (*gatewayv1.Namespace)(&e.Namespace.Name),
							Name:      "gwconf-upgrade-123-current",
						},
						ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
					},
				},
				&gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "gw-upgrade-123-current-",
						Labels: map[string]string{
							"gw-upgrade-123-current": "true",
						},
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName("gwclass-upgrade-123-current"),
						Listeners: []gatewayv1.Listener{{
							Name:     "http",
							Protocol: gatewayv1.HTTPProtocolType,
							Port:     gatewayv1.PortNumber(80),
						}},
					},
				},
			},
			assertionsAfterUpgrade: []assertion{
				{
					Name: "Gateway is programmed",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayAndItsListenersAreProgrammedAssertion("gw-upgrade-123-current=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "DataPlane deployment is not patched after operator upgrade",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						gatewayDataPlaneDeploymentIsNotPatched("gw-upgrade-123-current=true")(ctx, c, cl.MgrClient)
					},
				},
				{
					Name: "Cluster wide resources owned by the ControlPlane get the proper set of labels",
					Func: func(c *assert.CollectT, cl *testutils.K8sClients) {
						clusterWideResourcesAreProperlyManaged("gw-upgrade-123-current=true")(ctx, c, cl.MgrClient)
					},
				},
			},
		},
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
						gatewayDataPlaneDeploymentIsNotPatched("gw-upgrade-nightly-to-current=true")(ctx, c, cl.MgrClient)
					},
				},
			},
		},
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

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var (
				tag              string
				targetRepository = image
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
				"image.repository":                   image,
				"readinessProbe.initialDelaySeconds": "1",
				"readinessProbe.periodSeconds":       "1",
				// Disable leader election and anonymous reports for tests.
				"no_leader_election": "true",
				"anonymous_reports":  "false",
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

			// Deploy the objects that should be present before the upgrade.
			cl := client.NewNamespacedClient(e.Clients.MgrClient, e.Namespace.Name)
			for _, obj := range tc.objectsToDeploy {
				require.NoError(t, cl.Create(ctx, obj))
				t.Cleanup(func() {
					require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, obj)))
				})
			}

			require.NoError(t, waitForOperatorDeployment(t, ctx, e.Namespace.Name, e.Clients.K8sClient, waitTime,
				deploymentAssertConditions(t, deploymentReadyConditions()...),
			))

			opts.SetValues["image.tag"] = tag
			opts.SetValues["image.repository"] = targetRepository

			require.NoError(t, helm.UpgradeE(t, opts, chart, releaseName))
			require.NoError(t, waitForOperatorDeployment(t, ctx, e.Namespace.Name, e.Clients.K8sClient, waitTime,
				deploymentAssertConditions(t, deploymentReadyConditions()...),
			),
			)

			for _, assertion := range tc.assertionsAfterUpgrade {
				t.Run(assertion.Name, func(t *testing.T) {
					require.EventuallyWithT(t, func(c *assert.CollectT) {
						assertion.Func(c, e.Clients)
					}, waitTime, time.Second)
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
	if len(splitImage) != 2 {
		t.Fatalf("image %q does not contain a tag", image)
	}
	return splitImage[0], splitImage[1]
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
									Name:  consts.DataPlaneProxyContainerName,
									Image: helpers.GetDefaultDataPlaneImage(),
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
								Name:  consts.ControlPlaneControllerContainerName,
								Image: consts.DefaultControlPlaneImage,
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
	var gws gatewayv1.GatewayList
	lReq, err := labels.ParseToRequirements(gatewayLabelSelector)
	if err != nil {
		c.Errorf("failed to parse label selector %q: %v", gatewayLabelSelector, err)
		c.FailNow()
	}
	lSel := labels.NewSelector()
	for _, req := range lReq {
		lSel = lSel.Add(req)
	}

	require.NoError(c,
		cl.List(ctx, &gws, &client.ListOptions{
			LabelSelector: lSel,
		}),
	)
	require.Len(c, gws.Items, 1)
	return &gws.Items[0]
}

// gatewayAndItsListenersAreProgrammedAssertion returns a predicate that checks
// if the Gateway and its listeners are programmed.
func gatewayAndItsListenersAreProgrammedAssertion(gatewayLabelSelector string) func(context.Context, *assert.CollectT, client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		assert.True(c, gateway.IsProgrammed(gw))
		assert.True(c, gateway.AreListenersProgrammed(gw))
	}
}

// gatewayDataPlaneDeploymentIsNotPatched returns a predicate that checks if the
// DataPlane deployment is not patched.
func gatewayDataPlaneDeploymentIsNotPatched(gatewayLabelSelector string) func(context.Context, *assert.CollectT, client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)

		dataplanes, err := gateway.ListDataPlanesForGateway(ctx, cl, gw)
		if err != nil {
			c.Errorf("failed to list DataPlanes for Gateway %q: %v", client.ObjectKeyFromObject(gw), err)
			c.FailNow()
		}
		require.Len(c, dataplanes, 1)
		dp := &dataplanes[0]
		if dp.Generation != 1 {
			c.Errorf("DataPlane %q got patched but it shouldn't: %v", client.ObjectKeyFromObject(dp), err)
			c.FailNow()
		}
	}
}

func clusterWideResourcesAreProperlyManaged(gatewayLabelSelector string) func(ctx context.Context, c *assert.CollectT, cl client.Client) {
	return func(ctx context.Context, c *assert.CollectT, cl client.Client) {
		gw := getGatewayByLabelSelector(gatewayLabelSelector, ctx, c, cl)
		controlplanes, err := gateway.ListControlPlanesForGateway(ctx, cl, gw)
		if err != nil {
			c.Errorf("failed to list ControlPlanes for Gateway %q: %v", client.ObjectKeyFromObject(gw), err)
			c.FailNow()
		}
		require.Len(c, controlplanes, 1)
		cp := &controlplanes[0]

		managedByLabelSet := k8sutils.GetManagedByLabelSet(cp)

		clusterRoles, err := k8sutils.ListClusterRoles(
			ctx,
			cl,
			client.MatchingLabels(managedByLabelSet),
		)
		require.NoError(c, err)
		require.Len(c, clusterRoles, 1)

		clusterRoleBindings, err := k8sutils.ListClusterRoleBindings(
			ctx,
			cl,
			client.MatchingLabels(managedByLabelSet),
		)
		require.NoError(c, err)
		require.Len(c, clusterRoleBindings, 1)

		validatingWebhookConfigurations, err := k8sutils.ListValidatingWebhookConfigurations(
			ctx,
			cl,
			client.MatchingLabels(managedByLabelSet),
		)
		require.NoError(c, err)
		require.Len(c, validatingWebhookConfigurations, 1)
	}
}
