package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/kong/kubernetes-telemetry/pkg/forwarders"
	"github.com/kong/kubernetes-telemetry/pkg/serializers"
	"github.com/kong/kubernetes-telemetry/pkg/telemetry"
	"github.com/kong/kubernetes-telemetry/pkg/types"
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
	// Register DataPlane GVK in REST mapper.
	restMapper.Add(schema.GroupVersionKind{
		Group:   operatorv1beta1.SchemeGroupVersion.Group,
		Version: operatorv1beta1.SchemeGroupVersion.Version,
		Kind:    "DataPlane",
	}, meta.RESTScopeNamespace)
	return restMapper
}

func prepareControllerClient(scheme *runtime.Scheme) client.Client {
	return fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithRESTMapper(createRESTMapper()).
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
			name: "3 dataplanes, 2 nodes, 1 node",
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
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8sv=v1.27.2",
				"k8s_nodes_count=2",
				"k8s_pods_count=1",
				"k8s_dataplanes_count=3",
			},
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			scheme := prepareScheme(t)
			k8sclient := testk8sclient.NewSimpleClientset()
			ctrlClient := prepareControllerClient(scheme)

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
			select {
			case buf := <-ch:
				checkTelemetryReportContent(t, string(buf), tc.expectedReportParts...)
			case <-ctx.Done():
				t.Fatalf("context closed with error %v", ctx.Err())
			}
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
			select {
			case buf := <-ch:
				checkTelemetryReportContent(t, string(buf), tc.expectedReportParts...)
			case <-ctx.Done():
				t.Fatalf("context closed with error %v", ctx.Err())
			}

			tc.action(t, context.Background(), dyn)

			require.NoError(t, m.TriggerExecute(ctx, "test-signal"), "failed triggering signal execution")

			t.Log("checking received report...")
			select {
			case buf := <-ch:
				checkTelemetryReportContent(t, string(buf), tc.expectedReportPartsAfterAction...)
			case <-ctx.Done():
				t.Fatalf("context closed with error %v", ctx.Err())
			}
		})
	}
}

func checkTelemetryReportContent(t *testing.T, report string, containValue ...string) {
	t.Helper()

	for _, v := range containValue {
		require.Containsf(t, report, v, "report should contain %s", v)
	}
}
