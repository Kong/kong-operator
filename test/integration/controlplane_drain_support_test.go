package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kong/go-kong/kong"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	k8senvironments "github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/builder"
	"github.com/kong/kong-operator/v2/internal/annotations"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
)

func TestControlPlaneDrainSupport(t *testing.T) {
	t.Parallel()

	ctx := GetCtx()
	env := GetEnv()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	clients := GetClients()
	mgrClient := clients.MgrClient

	dataplane := builder.NewDataPlaneBuilder().
		WithObjectMeta(metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dp-drain-",
		}).
		WithPodTemplateSpec(&corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  consts.DataPlaneProxyContainerName,
						Image: helpers.GetDefaultDataPlaneImage(),
						ReadinessProbe: func() *corev1.Probe {
							probe := k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusEndpoint)
							probe.InitialDelaySeconds = 1
							probe.PeriodSeconds = 1
							return probe
						}(),
					},
				},
			},
		}).
		Build()
	dataplane.Spec.Deployment.Replicas = lo.ToPtr(int32(1))

	t.Log("creating dataplane resource")
	require.NoError(t, mgrClient.Create(ctx, dataplane))
	cleaner.Add(dataplane)

	dataplaneKey := client.ObjectKeyFromObject(dataplane)

	t.Log("waiting for dataplane deployment to be ready")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneKey, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	controlplane := &gwtypes.ControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwtypes.ControlPlaneGVR().GroupVersion().String(),
			Kind:       "ControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "cp-drain-",
		},
		Spec: gwtypes.ControlPlaneSpec{
			ControlPlaneOptions: gwtypes.ControlPlaneOptions{
				IngressClass: lo.ToPtr(ingressClass),
				Translation: &gwtypes.ControlPlaneTranslationOptions{
					DrainSupport: lo.ToPtr(gwtypes.ControlPlaneDrainSupportStateEnabled),
				},
			},
			DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
				Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
				Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
					Name: dataplane.Name,
				},
			},
		},
	}

	t.Log("creating controlplane resource")
	require.NoError(t, mgrClient.Create(ctx, controlplane))
	addToCleanup(t, mgrClient, controlplane)
	controlPlaneKey := client.ObjectKeyFromObject(controlplane)

	t.Log("waiting for controlplane to become scheduled and provisioned")
	require.Eventually(t, testutils.ControlPlaneIsScheduled(t, ctx, controlPlaneKey, clients.OperatorClient), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, ctx, controlPlaneKey, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneIsOptionsValid(t, ctx, controlPlaneKey, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	adminSecret := fetchControlPlaneAdminSecret(t, ctx, mgrClient, controlplane)

	backendDeployment, service := deployDrainSupportBackend(t, ctx, namespace.Name)
	cleaner.Add(backendDeployment)
	cleaner.Add(service)

	ingress := createDrainSupportIngress(t, ctx, namespace.Name, service)
	cleaner.Add(ingress)

	kongClient := buildKongAdminClient(t, ctx, controlplane.Namespace, dataplane.Name, adminSecret)

	upstream := waitForServiceUpstream(t, ctx, kongClient, service.Name, service.Namespace)

	podIPs := waitForInitialTargets(t, ctx, kongClient, upstream, 2)

	terminatingPodIP := podIPs[0]

	podsClient := env.Cluster().Client().CoreV1().Pods(namespace.Name)
	var podList *corev1.PodList
	require.Eventually(t, func() bool {
		list, err := podsClient.List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{"app": backendDeployment.Spec.Template.Labels["app"]}).String(),
		})
		if err != nil {
			t.Logf("error listing backend pods: %v", err)
			return false
		}
		readyWithIP := 0
		for _, pod := range list.Items {
			if pod.Status.PodIP != "" {
				readyWithIP++
			}
		}
		if readyWithIP >= 2 {
			podList = list
			return true
		}
		return false
	}, 2*time.Minute, time.Second)

	require.NotEmpty(t, podList.Items)
	var podName string
	for _, pod := range podList.Items {
		if pod.Status.PodIP == terminatingPodIP {
			podName = pod.Name
			break
		}
	}
	require.NotEmpty(t, podName)

	t.Logf("deleting pod %s to trigger termination", podName)
	require.NoError(t, podsClient.Delete(ctx, podName, metav1.DeleteOptions{}))

	require.Eventually(t, func() bool {
		pod, err := podsClient.Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		return pod.DeletionTimestamp != nil
	}, 2*time.Minute, time.Second)

	waitForTerminatingEndpoint(t, ctx, namespace.Name, service.Name, terminatingPodIP)

	waitForWeightZero(t, ctx, kongClient, upstream, terminatingPodIP)
}

func fetchControlPlaneAdminSecret(t *testing.T, ctx context.Context, cl client.Client, cp *gwtypes.ControlPlane) *corev1.Secret {
	t.Helper()

	var secrets corev1.SecretList
	require.Eventually(t, func() bool {
		secrets = corev1.SecretList{}
		err := cl.List(ctx, &secrets, client.InNamespace(cp.Namespace), client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
			consts.SecretUsedByServiceLabel:      consts.ControlPlaneServiceKindAdmin,
		})
		if err != nil {
			return false
		}
		return len(secrets.Items) > 0
	}, testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	return secrets.Items[0].DeepCopy()
}

func deployDrainSupportBackend(t *testing.T, ctx context.Context, namespace string) (*appsv1.Deployment, *corev1.Service) {
	t.Helper()

	labels := map[string]string{"app": "drain-backend"}

	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	container.Lifecycle = &corev1.Lifecycle{
		PreStop: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", "sleep 30"}},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "drain-backend-",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: lo.ToPtr(int32(2)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: lo.ToPtr(int64(90)),
					Containers:                    []corev1.Container{container},
				},
			},
		},
	}

	k8sClient := GetEnv().Cluster().Client()
	createdDeployment, err := k8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: "drain-service-",
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(80),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	createdService, err := k8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	return createdDeployment, createdService
}

func createDrainSupportIngress(t *testing.T, ctx context.Context, namespace string, svc *corev1.Service) client.Object {
	t.Helper()

	clusterVersion, err := GetEnv().Cluster().Version()
	require.NoError(t, err)

	ingress := generators.NewIngressForServiceWithClusterVersion(
		clusterVersion,
		fmt.Sprintf("/%s", "drain"),
		map[string]string{"konghq.com/strip-path": "true", annotations.IngressClassKey: ingressClass},
		svc,
	)

	require.NoError(t, clusters.DeployIngress(ctx, GetEnv().Cluster(), namespace, ingress))
	ingress.(client.Object).SetNamespace(namespace)

	return ingress.(client.Object)
}

func buildKongAdminClient(t *testing.T, ctx context.Context, namespace, dataplaneName string, adminSecret *corev1.Secret) *kong.Client {
	t.Helper()

	env := GetEnv()
	podsClient := env.Cluster().Client().CoreV1().Pods(namespace)

	var selectedPod corev1.Pod
	require.Eventually(t, func() bool {
		podList, err := podsClient.List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{"app": dataplaneName}).String(),
		})
		if err != nil {
			t.Logf("error listing dataplane pods: %v", err)
			return false
		}
		for _, pod := range podList.Items {
			if isPodReady(&pod) {
				selectedPod = pod
				return true
			}
		}
		return false
	}, testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	kubeconfigPath := writeTemporaryKubeconfig(t, env)
	forwardCtx, cancel := context.WithCancel(context.Background())
	localPort := startPortForward(forwardCtx, t, kubeconfigPath, namespace, fmt.Sprintf("pod/%s", selectedPod.Name), consts.DataPlaneAdminAPIPort)

	tlsConfig := buildAdminTLSConfig(t, adminSecret)
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	adminURL := fmt.Sprintf("https://127.0.0.1:%d", localPort)
	kongClient, err := kong.NewClient(&adminURL, httpClient)
	require.NoError(t, err)

	t.Cleanup(func() {
		cancel()
		time.Sleep(100 * time.Millisecond)
		_ = os.Remove(kubeconfigPath)
	})

	return kongClient
}

func waitForServiceUpstream(t *testing.T, ctx context.Context, kongClient *kong.Client, serviceName, namespace string) *kong.Upstream {
	t.Helper()

	var upstream *kong.Upstream
	require.Eventually(t, func() bool {
		upstreams, err := kongClient.Upstreams.ListAll(ctx)
		if err != nil {
			t.Logf("error listing upstreams: %v", err)
			return false
		}
		for _, u := range upstreams {
			if hasTag(u.Tags, fmt.Sprintf("k8s-name:%s", serviceName)) &&
				hasTag(u.Tags, fmt.Sprintf("k8s-namespace:%s", namespace)) &&
				hasTag(u.Tags, "k8s-kind:Service") {
				upstream = u
				return true
			}
		}
		return false
	}, 2*time.Minute, time.Second)

	return upstream
}

func waitForInitialTargets(t *testing.T, ctx context.Context, kongClient *kong.Client, upstream *kong.Upstream, expected int) []string {
	t.Helper()

	var podIPs []string
	upstreamID := upstreamIdentifier(upstream)
	require.Eventually(t, func() bool {
		nodes, err := kongClient.UpstreamNodeHealth.ListAll(ctx, upstreamID)
		if err != nil {
			t.Logf("error listing upstream node health: %v", err)
			return false
		}
		if len(nodes) != expected {
			t.Logf("waiting for %d nodes, currently have %d", expected, len(nodes))
			return false
		}
		ips := make([]string, 0, len(nodes))
		for _, node := range nodes {
			if node.Target == nil {
				return false
			}
			parts := strings.Split(*node.Target, ":")
			if len(parts) != 2 {
				t.Logf("unexpected target format: %s", *node.Target)
				return false
			}
			if node.Weight != nil && *node.Weight == 0 {
				t.Logf("node %s already has zero weight", *node.Target)
				return false
			}
			ips = append(ips, parts[0])
		}
		podIPs = ips
		return true
	}, 2*time.Minute, time.Second)

	return podIPs
}

func waitForTerminatingEndpoint(t *testing.T, ctx context.Context, namespace, serviceName, terminatingIP string) {
	t.Helper()

	sliceClient := GetEnv().Cluster().Client().DiscoveryV1().EndpointSlices(namespace)
	selector := labels.SelectorFromSet(map[string]string{discoveryv1.LabelServiceName: serviceName}).String()

	require.Eventually(t, func() bool {
		sliceList, err := sliceClient.List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			t.Logf("error listing endpointslices: %v", err)
			return false
		}
		for _, slice := range sliceList.Items {
			for _, endpoint := range slice.Endpoints {
				if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating &&
					slices.Contains(endpoint.Addresses, terminatingIP) {
					return true
				}
			}
		}
		return false
	}, 2*time.Minute, 500*time.Millisecond)
}

func waitForWeightZero(t *testing.T, ctx context.Context, kongClient *kong.Client, upstream *kong.Upstream, terminatingIP string) {
	t.Helper()

	upstreamID := upstreamIdentifier(upstream)
	require.Eventually(t, func() bool {
		nodes, err := kongClient.UpstreamNodeHealth.ListAll(ctx, upstreamID)
		if err != nil {
			t.Logf("error listing upstream node health: %v", err)
			return false
		}
		found := false
		for _, node := range nodes {
			if node.Target == nil {
				continue
			}
			if strings.HasPrefix(*node.Target, terminatingIP+":") {
				if node.Weight == nil || *node.Weight != 0 {
					return false
				}
				found = true
			}
		}
		return found
	}, 2*time.Minute, time.Second)
}

func hasTag(tags []*string, value string) bool {
	return slices.ContainsFunc(tags, func(tag *string) bool {
		return tag != nil && *tag == value
	})
}

func upstreamIdentifier(upstream *kong.Upstream) *string {
	if upstream.ID != nil {
		return upstream.ID
	}
	return upstream.Name
}

func writeTemporaryKubeconfig(t *testing.T, env k8senvironments.Environment) string {
	t.Helper()

	kubeconfigBytes, err := generators.NewKubeConfigForRestConfig(env.Name(), env.Cluster().Config())
	require.NoError(t, err)

	f, err := os.CreateTemp(t.TempDir(), "kubeconfig-")
	require.NoError(t, err)
	defer f.Close()

	_, err = f.Write(kubeconfigBytes)
	require.NoError(t, err)

	return f.Name()
}

func startPortForward(ctx context.Context, t *testing.T, kubeconfigPath, namespace, targetRef string, targetPort int) int {
	t.Helper()

	localPort := getFreePort(t)
	args := []string{"--kubeconfig", kubeconfigPath, "port-forward", "-n", namespace, targetRef, fmt.Sprintf("%d:%d", localPort, targetPort)}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	require.NoError(t, cmd.Start())

	go func() {
		err := cmd.Wait()
		if err != nil && ctx.Err() == nil {
			t.Logf("port-forward exited unexpectedly: %v\n%s", err, output.String())
		}
	}()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 30*time.Second, 200*time.Millisecond)

	return localPort
}

func getFreePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

func buildAdminTLSConfig(t *testing.T, secret *corev1.Secret) *tls.Config {
	t.Helper()

	crt, ok := secret.Data[corev1.TLSCertKey]
	require.True(t, ok, "admin secret missing tls.crt")
	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
	require.True(t, ok, "admin secret missing tls.key")

	cert, err := tls.X509KeyPair(crt, key)
	require.NoError(t, err)

	ca, ok := secret.Data["ca.crt"]
	require.True(t, ok, "admin secret missing ca.crt")

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(ca))

	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec // Test establishes TLS via port-forwarded localhost; self-signed cert hostname doesn't match.
	}
}

func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
