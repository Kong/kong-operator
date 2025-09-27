package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	gov2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
)

// TestControlPlaneDrainSupport verifies that when ControlPlane translation.drainSupport is enabled,
// terminating endpoints are included in Kong upstreams with weight=0 for graceful draining.
func TestControlPlaneDrainSupport(t *testing.T) {
	t.Parallel()
	ctx := GetCtx()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, GetEnv())
	cl := GetClients().MgrClient

	// 1) Create DataPlane
	dp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dp-drain-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  consts.DataPlaneProxyContainerName,
									Image: helpers.GetDefaultDataPlaneImage(),
								}},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, dp))
	cleaner.Add(dp)
	dpKey := client.ObjectKeyFromObject(dp)
	require.Eventually(t, testutils.DataPlaneIsReady(t, ctx, dpKey, GetClients().OperatorClient), testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	// 2) Create ControlPlane with drainSupport and config dump enabled
	cp := &gwtypes.ControlPlane{
		TypeMeta: metav1.TypeMeta{APIVersion: gwtypes.ControlPlaneGVR().GroupVersion().String(), Kind: "ControlPlane"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "cp-drain-",
		},
		Spec: gwtypes.ControlPlaneSpec{
			DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
				Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
				Ref:  &gwtypes.ControlPlaneDataPlaneTargetRef{Name: dp.Name},
			},
			ControlPlaneOptions: gwtypes.ControlPlaneOptions{
				IngressClass: lo.ToPtr(ingressClass),
				Translation: &gov2beta1.ControlPlaneTranslationOptions{
					DrainSupport: lo.ToPtr(gov2beta1.ControlPlaneDrainSupportStateEnabled),
				},
				ConfigDump: &gov2beta1.ControlPlaneConfigDump{
					State:         gov2beta1.ConfigDumpStateEnabled,
					DumpSensitive: gov2beta1.ConfigDumpStateDisabled,
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cp))
	cleaner.Add(cp)
	cpKey := client.ObjectKeyFromObject(cp)
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, ctx, cpKey, GetClients()), 3*time.Minute, testutils.ControlPlaneCondTick)

	// 3) Deploy a backend (Deployment + Service) and expose via Ingress
	container := generators.NewContainer("echo", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err := GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(deployment)

	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	service, err = GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(service)

	// Create an Ingress that routes to the service to ensure an upstream is created
	kubernetesVersion, err := GetEnv().Cluster().Version()
	require.NoError(t, err)
	ing := generators.NewIngressForServiceWithClusterVersion(
		kubernetesVersion,
		fmt.Sprintf("/%s-echo", uuid.NewString()[:8]),
		map[string]string{"kubernetes.io/ingress.class": ingressClass, "konghq.com/strip-path": "true"},
		service,
	)
	require.NoError(t, clusters.DeployIngress(ctx, GetEnv().Cluster(), namespace.Name, ing))
	// Set the namespace so that the cleaner can delete it later without errors.
	ing.(client.Object).SetNamespace(namespace.Name)
	cleaner.Add(ing.(client.Object))

	// Wait for DP to be ready still and for config to be generated
	require.Eventually(t, testutils.ControlPlaneIsOptionsValid(t, ctx, cpKey, GetClients()), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	// 4) Identify one pod backing the service and delete it to make it terminating
	pods, err := GetEnv().Cluster().Client().CoreV1().Pods(namespace.Name).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(service.Spec.Selector).String(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, pods.Items)
	podToDrain := pods.Items[0]
	// Record its name/IP for matching the target in config dump later
	podName := podToDrain.Name
	var podIP string
	// Ensure we have the Pod IP; wait a bit if not yet assigned
	require.Eventually(t, func() bool {
		p, err := GetEnv().Cluster().Client().CoreV1().Pods(namespace.Name).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		if p.Status.PodIP == "" {
			return false
		}
		podIP = p.Status.PodIP
		return true
	}, time.Minute, time.Second)
	require.NotEmpty(t, podIP)
	// Delete the pod to mark its endpoint as terminating
	require.NoError(t, GetEnv().Cluster().Client().CoreV1().Pods(namespace.Name).Delete(ctx, podName, metav1.DeleteOptions{}))

	// 5) Poll the ControlPlane diagnostics config dump and assert the terminating endpoint target has weight=0
	diagURL := fmt.Sprintf("http://127.0.0.1:10256/debug/controlplanes/namespace/%s/name/%s/config/successful", namespace.Name, cp.Name)
	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	t.Logf("fetching config dump from %s and waiting for terminating target %s:80 to have weight=0", diagURL, podIP)
	require.Eventually(t, func() bool {
		// Fetch last successful config dump
		resp, err := httpClient.Get(diagURL)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		var payload struct {
			Config struct {
				Upstreams []struct {
					Name    *string `json:"name"`
					Targets []struct {
						Target *string `json:"target"`
						Weight *int    `json:"weight,omitempty"`
					} `json:"targets,omitempty"`
				} `json:"upstreams,omitempty"`
			} `json:"config"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return false
		}
		targetToFind := fmt.Sprintf("%s:%d", podIP, 80)
		for _, ups := range payload.Config.Upstreams {
			for _, tgt := range ups.Targets {
				if tgt.Target != nil && *tgt.Target == targetToFind {
					// Found the target that should be marked drained
					if tgt.Weight != nil && *tgt.Weight == 0 {
						return true
					}
					// Found target but weight != 0 yet
					return false
				}
			}
		}
		// Not found yet, keep polling
		return false
	}, 2*time.Minute, 2*time.Second)
}
