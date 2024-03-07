package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/kong/kubernetes-telemetry/pkg/forwarders"
	"github.com/kong/kubernetes-telemetry/pkg/serializers"
	"github.com/kong/kubernetes-telemetry/pkg/telemetry"
	"github.com/kong/kubernetes-telemetry/pkg/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	testdynclient "k8s.io/client-go/dynamic/fake"
	testk8sclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
)

func prepareScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, testk8sclient.AddToScheme(scheme))
	require.NoError(t, operatorv1beta1.AddToScheme(scheme))
	return scheme
}

func createRESTMapper() meta.RESTMapper {
	restMapper := meta.NewDefaultRESTMapper(nil)
	// Register GVKs in REST mapper.
	restMapper.Add(schema.GroupVersionKind{
		Group:   operatorv1beta1.SchemeGroupVersion.Group,
		Version: operatorv1beta1.SchemeGroupVersion.Version,
		Kind:    "DataPlane",
	}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{
		Group:   operatorv1beta1.SchemeGroupVersion.Group,
		Version: operatorv1beta1.SchemeGroupVersion.Version,
		Kind:    "ControlPlane",
	}, meta.RESTScopeNamespace)
	return restMapper
}

func prepareControllerClient(scheme *runtime.Scheme, objects ...runtime.Object) client.Client {
	return fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithRESTMapper(createRESTMapper()).
		WithRuntimeObjects(objects...).
		Build()
}

func versionInfo() *version.Info {
	return &version.Info{
		Major:      "1",
		Minor:      "27",
		GitVersion: "v1.27.2",
		Platform:   "linux/amd64",
	}
}

func TestCreateManager(t *testing.T) {
	payload := types.ProviderReport{
		"v": "0.6.2",
	}

	testcases := []struct {
		name                string
		objects             []runtime.Object
		expectedReportParts []string
	}{
		{
			name: "0 dataplanes, 0 pods, 1 node",
			objects: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-0",
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8sv=v1.27.2",
				"k8s_nodes_count=1",
				"k8s_pods_count=0",
				"k8s_dataplanes_count=0",
			},
		},
		{
			name: "1 dataplane, 1 pod, 1 node",
			objects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "gateway-operator-abcde",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-0",
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "cloud-gateway-0",
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8sv=v1.27.2",
				"k8s_nodes_count=1",
				"k8s_pods_count=1",
				"k8s_dataplanes_count=1",
			},
		},
		{
			name: "4 dataplanes (1 owned), 3 controlplanes (1 owned), 2 nodes, 1 pod",
			objects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "gateway-operator-abcde",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-0",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "cloud-gateway-0",
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "cloud-gateway-1",
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "cloud-gateway-2",
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "kong",
						Name:            "owned-cloud-gateway-3",
						OwnerReferences: []metav1.OwnerReference{{}}, // Owned by something, we don't care what.
					},
				},
				&operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "control-plane-0",
					},
				},
				&operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "control-plane-1",
					},
				},
				&operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "kong",
						Name:            "owned-control-plane-2",
						OwnerReferences: []metav1.OwnerReference{{}}, // Owned by something, we don't care what.
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8sv=v1.27.2",
				"k8s_nodes_count=2",
				"k8s_pods_count=1",
				"k8s_dataplanes_count=4",
				"k8s_controlplanes_count=3",
				"k8s_standalone_dataplanes_count=3",
				"k8s_standalone_controlplanes_count=2",
			},
		},
		{
			name: "dataplanes replicas count",
			objects: []runtime.Object{
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "dp-10-replicas",
					},
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									Replicas: lo.ToPtr[int32](10),
								},
							},
						},
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "dp-5-scaling-max-replicas",
					},
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									Scaling: &operatorv1beta1.Scaling{
										HorizontalScaling: &operatorv1beta1.HorizontalScaling{
											MaxReplicas: 5,
										},
									},
								},
							},
						},
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "dp-no-replicas", // No replicas or scaling defined counts as 1.
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"k8s_dataplanes_requested_replicas_count=16",
			},
		},
		{
			name: "controlplane replicas count",
			objects: []runtime.Object{
				&operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "cp-10-replicas",
					},
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
							Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
								Replicas: lo.ToPtr[int32](10),
							},
						},
					},
				},
				&operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "cp-no-replicas", // No replicas counts as 1.
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"k8s_controlplanes_requested_replicas_count=11",
			},
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			scheme := prepareScheme(t)
			k8sclient := testk8sclient.NewSimpleClientset()

			ctrlClient := prepareControllerClient(scheme, tc.objects...)

			d, ok := k8sclient.Discovery().(*fakediscovery.FakeDiscovery)
			require.True(t, ok)
			d.FakedServerVersion = versionInfo()

			dyn := testdynclient.NewSimpleDynamicClient(scheme, tc.objects...)
			m, err := createManager(
				types.Signal(SignalPing), k8sclient, ctrlClient, dyn, payload,
				telemetry.OptManagerPeriod(time.Hour),
				telemetry.OptManagerLogger(logr.Discard()),
			)
			require.NoError(t, err, "creating telemetry manager failed")
			ch := make(chan []byte)
			consumer := telemetry.NewConsumer(
				serializers.NewSemicolonDelimited(),
				forwarders.NewChannelForwarder(ch),
			)
			require.NoError(t, m.AddConsumer(consumer), "failed adding consumer to telemetry manager")

			t.Log("trigger a report...")
			require.NoError(t, m.Start())
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, m.TriggerExecute(ctx, "test-signal"), "failed triggering signal execution")

			t.Log("checking received report...")
			requireReportContainsValuesEventually(t, ch, tc.expectedReportParts...)
		})
	}
}

func TestTelemetryUpdates(t *testing.T) {
	payload := types.ProviderReport{
		"v": "0.6.2",
	}

	testcases := []struct {
		name                           string
		objects                        []runtime.Object
		expectedReportParts            []string
		action                         func(t *testing.T, ctx context.Context, dyn *testdynclient.FakeDynamicClient)
		expectedReportPartsAfterAction []string
	}{
		{
			name: "1 dataplane which gets deleted",
			objects: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-0",
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "cloud-gateway-0",
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8s_nodes_count=1",
				"k8s_pods_count=0",
				"k8s_dataplanes_count=1",
			},
			action: func(t *testing.T, ctx context.Context, dyn *testdynclient.FakeDynamicClient) {
				require.NoError(t, dyn.Resource(dataplaneGVR).Namespace("kong").Delete(ctx, "cloud-gateway-0", metav1.DeleteOptions{}))
			},
			expectedReportPartsAfterAction: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8s_nodes_count=1",
				"k8s_pods_count=0",
				"k8s_dataplanes_count=0",
			},
		},
		{
			name:    "0 dataplane and then 2 get added",
			objects: []runtime.Object{},
			expectedReportParts: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8s_pods_count=0",
				"k8s_dataplanes_count=0",
			},
			action: func(t *testing.T, ctx context.Context, dyn *testdynclient.FakeDynamicClient) {
				_, err := dyn.Resource(dataplaneGVR).Namespace("kong").Create(ctx, &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "gateway-operator.konghq.com/v1beta1",
						"kind":       "DataPlane",
						"metadata": map[string]interface{}{
							"name":      "cloud-gateway-0",
							"namespace": "kong",
						},
					},
				}, metav1.CreateOptions{})
				require.NoError(t, err)
				_, err = dyn.Resource(dataplaneGVR).Namespace("kong").Create(ctx, &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "gateway-operator.konghq.com/v1beta1",
						"kind":       "DataPlane",
						"metadata": map[string]interface{}{
							"name":      "cloud-gateway-1",
							"namespace": "kong",
						},
					},
				}, metav1.CreateOptions{})
				require.NoError(t, err)
			},
			expectedReportPartsAfterAction: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8s_pods_count=0",
				"k8s_dataplanes_count=2",
			},
		},
	}
	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			scheme := prepareScheme(t)
			k8sclient := testk8sclient.NewSimpleClientset()
			ctrlClient := prepareControllerClient(scheme)

			dClient := k8sclient.Discovery()
			d, ok := dClient.(*fakediscovery.FakeDiscovery)
			require.True(t, ok)
			d.FakedServerVersion = versionInfo()

			dyn := testdynclient.NewSimpleDynamicClient(scheme, tc.objects...)
			m, err := createManager(
				types.Signal(SignalPing), k8sclient, ctrlClient, dyn, payload,
				telemetry.OptManagerPeriod(time.Hour),
				telemetry.OptManagerLogger(logr.Discard()),
			)
			require.NoError(t, err, "creating telemetry manager failed")
			ch := make(chan []byte)
			consumer := telemetry.NewConsumer(
				serializers.NewSemicolonDelimited(),
				forwarders.NewChannelForwarder(ch),
			)
			require.NoError(t, m.AddConsumer(consumer), "failed adding consumer to telemetry manager")

			t.Log("trigger a report...")
			require.NoError(t, m.Start())
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, m.TriggerExecute(ctx, "test-signal"), "failed triggering signal execution")

			t.Log("checking received report...")
			requireReportContainsValuesEventually(t, ch, tc.expectedReportParts...)

			tc.action(t, context.Background(), dyn)

			require.NoError(t, m.TriggerExecute(ctx, "test-signal"), "failed triggering signal execution")

			t.Log("checking received report...")
			requireReportContainsValuesEventually(t, ch, tc.expectedReportPartsAfterAction...)
		})
	}
}

func requireReportContainsValuesEventually(t *testing.T, ch <-chan []byte, containValue ...string) {
	const (
		waitTime = 3 * time.Second
		tickTime = 10 * time.Millisecond
	)
	require.Eventuallyf(t, func() bool {
		select {
		case report := <-ch:
			for _, v := range containValue {
				if !strings.Contains(string(report), v) {
					t.Logf("report should contain %s, actual: %s", v, string(report))
					return false
				}
			}
		case <-time.After(tickTime):
			return false
		}
		return true
	}, waitTime, tickTime, "telemetry report never matched expected value")
}
