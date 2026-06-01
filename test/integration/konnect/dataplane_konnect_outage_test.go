package konnect

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/pkg/consts"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestDataPlaneKonnectConfigurationSurvivesKonnectOutage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	clients := integration.GetClients()
	cl := clients.MgrClient
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	clientNamespaced := client.NewNamespacedClient(cl, namespace.Name)

	// Generate a test ID for labeling resources in order to easily identify them in Konnect.
	testID := uuid.NewString()[:8]
	t.Logf("Test ID: %s", testID)

	authCfg := deploy.KonnectAPIAuthConfiguration(
		t, ctx, clientNamespaced,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectAPIAuthConfigurationWithTestToken(test.KonnectAccessToken(), test.KonnectServerURL()),
	)

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	gatewayConfig.Spec.Konnect = &operatorv2beta1.KonnectOptions{
		APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
			Name: authCfg.Name,
		},
	}
	t.Logf("Deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	require.NoError(t, clientNamespaced.Create(ctx, gatewayConfig))
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass.Spec.ParametersRef = &gatewayv1.ParametersReference{
		Group:     "gateway-operator.konghq.com",
		Kind:      "GatewayConfiguration",
		Name:      gatewayConfig.Name,
		Namespace: (*gatewayv1.Namespace)(&namespace.Name),
	}
	t.Logf("Deploying GatewayClass %s", gatewayClass.Name)
	require.NoError(t, cl.Create(ctx, gatewayClass))
	cleaner.Add(gatewayClass)

	gatewayNN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gateway := helpers.GenerateGateway(gatewayNN, gatewayClass)
	t.Logf("Deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	require.NoError(t, clientNamespaced.Create(ctx, gateway))
	cleaner.Add(gateway)

	t.Log("Deploying backend deployment for HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	require.NoError(t, clientNamespaced.Create(ctx, deployment))
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	require.NoError(t, clientNamespaced.Create(ctx, service))

	httpRoute := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name)
	t.Logf("Deploying HTTPRoute %s/%s to access deployment %s via kong", httpRoute.Namespace, httpRoute.Name, deployment.Name)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.NoError(c, clientNamespaced.Create(ctx, httpRoute), "failed to deploy HTTPRoute %s/%s", httpRoute.Namespace, httpRoute.Name)
		cleaner.Add(httpRoute)
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

	t.Log("Waiting for Gateway to be programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNN, cl), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("Waiting for Gateway to get an IP address")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNN, cl)
	proxyURL := "http://" + gateway.Status.Addresses[0].Value

	t.Log("Get underlying DataPlane")
	dataPlanes := testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, dataPlanes, 1)
	dataplane := dataPlanes[0]

	t.Log("Get underlying KonnectGatewayControlPlane")
	konnectGatewayControlPlanes := testutils.MustListKonnectGatewayControlPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, konnectGatewayControlPlanes, 1)
	konnectGatewayControlPlane := konnectGatewayControlPlanes[0]

	t.Log("Waiting for DataPlane deployment to be available")
	var dataPlaneDeployment appsv1.Deployment
	require.Eventually(
		t,
		testutils.DataPlaneHasActiveDeployment(
			t, ctx, client.ObjectKeyFromObject(&dataplane), &dataPlaneDeployment, client.MatchingLabels{
				consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			}, clients,
		),
		waitTime, tickTime,
	)

	t.Log("Verifying initial DataPlane configuration works")
	assertKonnectProvidedConfigurationWorks(t, ctx, proxyURL)

	t.Log("Blocking all namespace egress to Konnect to simulate full outage")
	blockExternalEgressForNamespace(t, ctx, cl, cleaner, namespace.Name)

	t.Log("Get Konnect URL")
	konnectURL := konnectGatewayControlPlane.Status.ServerURL
	require.NotEmpty(t, konnectURL, "Konnect URL should not be empty")

	t.Logf("Waiting for egress block to be enforced in namespace %q", namespace.Name)
	waitForExternalEgressBlocked(t, ctx, cl, cleaner, namespace.Name, konnectURL)

	t.Log("Verifying DataPlane configuration still works while Konnect is unreachable")
	assertKonnectProvidedConfigurationWorks(t, ctx, proxyURL)

	t.Log("Restarting DataPlane deployment while Konnect is unreachable")
	restartDeploymentAndWait(t, ctx, cl, &dataPlaneDeployment)

	t.Log("Verifying DataPlane configuration still works after restart")
	assertKonnectProvidedConfigurationWorks(t, ctx, proxyURL)
}

func assertKonnectProvidedConfigurationWorks(
	t *testing.T,
	ctx context.Context,
	proxyURL string,
) {
	t.Helper()

	require.Eventuallyf(
		t,
		asserts.Expect404WithNoRouteFunc(t, ctx, proxyURL),
		waitTime, tickTime, "expected no-route response from %s", proxyURL,
	)

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	require.Eventually(
		t,
		testutils.GetResponseBodyContains(
			t, httpClient, helpers.MustBuildRequest(t, ctx, http.MethodGet, proxyURL+"/test", ""),
			"<title>httpbin.org</title>",
		),
		waitTime,
		tickTime,
	)

}

// blockExternalEgressForNamespace creates a NetworkPolicy in the given namespace that blocks
// all Pods in that namespace from reaching external (non-cluster, non-private) IP addresses.
// NetworkPolicy is namespace-scoped in Kubernetes, so one policy per namespace is required.
func blockExternalEgressForNamespace(
	t *testing.T, ctx context.Context, cl client.Client, cleaner *clusters.Cleaner, namespace string,
) {
	t.Helper()

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "block-external-egress",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // Empty selector matches all Pods in namespace.
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					To: []networkingv1.NetworkPolicyPeer{
						{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
						{IPBlock: &networkingv1.IPBlock{CIDR: "172.16.0.0/12"}},
						{IPBlock: &networkingv1.IPBlock{CIDR: "192.168.0.0/16"}},
					},
				},
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{},
							PodSelector:       &metav1.LabelSelector{},
						},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, policy))
	cleaner.Add(policy)
}

// waitForExternalEgressBlocked verifies that the NetworkPolicy is actively enforced
// for Pods in the given namespace by running a curl probe pod and waiting until it
// cannot connect to the given probeTarget URL. It retries up to maxEgressProbeAttempts
// times to account for Calico propagation delay on newly started Pods.
func waitForExternalEgressBlocked(
	t *testing.T, ctx context.Context, cl client.Client, cleaner *clusters.Cleaner, namespace, probeTarget string,
) {
	t.Helper()

	const (
		maxEgressProbeAttempts = 3
		connectTimeoutSecs     = "10"
		curlImage              = "curlimages/curl:latest"
	)

	for attempt := range maxEgressProbeAttempts {
		podName := "egress-probe-" + uuid.NewString()[:8]
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name:            "probe",
					Image:           curlImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"curl", "--max-time", connectTimeoutSecs, "--silent", probeTarget},
				}},
			},
		}
		require.NoError(t, cl.Create(ctx, pod))
		cleaner.Add(pod)

		var phase corev1.PodPhase
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			p := &corev1.Pod{}
			require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: namespace}, p))
			phase = p.Status.Phase
			assert.True(t, phase == corev1.PodSucceeded || phase == corev1.PodFailed,
				"pod %s not yet terminated, phase: %s", pod.Name, phase)
		}, waitTime, tickTime)

		if phase == corev1.PodFailed {
			t.Logf(
				"attempt %d/%d: egress probe in namespace %q failed to connect to %s - block enforced",
				attempt+1, maxEgressProbeAttempts, namespace, probeTarget,
			)
			return
		}
		t.Logf(
			"attempt %d/%d: egress probe in namespace %q connected to %s - Calico not yet enforced, retrying",
			attempt+1, maxEgressProbeAttempts, namespace, probeTarget,
		)
	}
	require.Failf(
		t, "external egress is still reachable",
		"Pods in namespace %q can still connect to %s after %d attempts - NetworkPolicy not enforced",
		namespace, probeTarget, maxEgressProbeAttempts,
	)
}

func restartDeploymentAndWait(
	t *testing.T, ctx context.Context, cl client.Client, deployment *appsv1.Deployment,
) {
	t.Helper()

	deploymentKey := client.ObjectKeyFromObject(deployment)
	restartedAt := time.Now().UTC().Format(time.RFC3339Nano)

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		latest := &appsv1.Deployment{}
		err := cl.Get(ctx, deploymentKey, latest)
		require.NoError(t, err)

		if latest.Spec.Template.Annotations == nil {
			latest.Spec.Template.Annotations = map[string]string{}
		}
		latest.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = restartedAt
		require.NoError(t, cl.Update(ctx, latest))
	}, waitTime, tickTime)

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		latest := &appsv1.Deployment{}
		err := cl.Get(ctx, deploymentKey, latest)
		require.NoError(t, err)

		replicas := int32(1)
		if latest.Spec.Replicas != nil {
			replicas = *latest.Spec.Replicas
		}

		assert.GreaterOrEqual(t, latest.Status.ObservedGeneration, latest.Generation)
		assert.Equal(t, replicas, latest.Status.UpdatedReplicas)
		assert.Equal(t, replicas, latest.Status.ReadyReplicas)
		assert.Equal(t, replicas, latest.Status.AvailableReplicas)
	}, waitTime, tickTime)
}
