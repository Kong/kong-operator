package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/kong/kubernetes-telemetry/pkg/forwarders"
	"github.com/kong/kubernetes-telemetry/pkg/serializers"
	"github.com/kong/kubernetes-telemetry/pkg/telemetry"
	"github.com/kong/kubernetes-telemetry/pkg/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
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

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func createRESTMapper() meta.RESTMapper {
	restMapper := meta.NewDefaultRESTMapper(nil)
	// Register GVKs in REST mapper.
	restMapper.Add(schema.GroupVersionKind{
		Group:   operatorv1beta1.SchemeGroupVersion.Group,
		Version: operatorv1beta1.SchemeGroupVersion.Version,
		Kind:    "DataPlane",
	}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{
		Group:   gwtypes.ControlPlaneGVR().Group,
		Version: gwtypes.ControlPlaneGVR().Version,
		Kind:    "ControlPlane",
	}, meta.RESTScopeNamespace)
	restMapper.AddSpecific(
		schema.GroupVersionKind{
			Group:   operatorv1alpha1.SchemeGroupVersion.Group,
			Version: operatorv1alpha1.SchemeGroupVersion.Version,
			Kind:    "AIGateway",
		},
		schema.GroupVersionResource{
			Group:    operatorv1alpha1.SchemeGroupVersion.Group,
			Version:  operatorv1alpha1.SchemeGroupVersion.Version,
			Resource: "aigateways",
		},
		schema.GroupVersionResource{
			Group:    operatorv1alpha1.SchemeGroupVersion.Group,
			Version:  operatorv1alpha1.SchemeGroupVersion.Version,
			Resource: "aigateway",
		},
		meta.RESTScopeNamespace,
	)

	restMapper.Add(schema.GroupVersionKind{
		Group:   configurationv1alpha1.SchemeGroupVersion.Group,
		Version: configurationv1alpha1.SchemeGroupVersion.Version,
		Kind:    configurationv1alpha1.KongRoute{}.GetTypeName(),
	}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{
		Group:   configurationv1alpha1.SchemeGroupVersion.Group,
		Version: configurationv1alpha1.SchemeGroupVersion.Version,
		Kind:    configurationv1alpha1.KongService{}.GetTypeName(),
	}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{
		Group:   configurationv1alpha1.SchemeGroupVersion.Group,
		Version: configurationv1alpha1.SchemeGroupVersion.Version,
		Kind:    configurationv1alpha1.KongSNI{}.GetTypeName(),
	}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{
		Group:   configurationv1.SchemeGroupVersion.Group,
		Version: configurationv1.SchemeGroupVersion.Version,
		Kind:    configurationv1.KongConsumer{}.GetTypeName(),
	}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{
		Group:   configurationv1beta1.SchemeGroupVersion.Group,
		Version: configurationv1beta1.SchemeGroupVersion.Version,
		Kind:    configurationv1beta1.KongConsumerGroup{}.GetTypeName(),
	}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{
		Group:   konnectv1alpha1.SchemeGroupVersion.Group,
		Version: konnectv1alpha1.SchemeGroupVersion.Version,
		Kind:    konnectv1alpha1.KonnectGatewayControlPlane{}.GetTypeName(),
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
				"k8s_ingresses_count=0",
				"k8s_pods_count=0",
				"k8s_dataplanes_count=0",
				"controller_dataplane_enabled=true",
				"controller_dataplane_bg_enabled=false",
				"controller_controlplane_enabled=false",
				"controller_gateway_enabled=false",
				"controller_konnect_enabled=true",
				"controller_kongplugininstallation_enabled=false",
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
				&netv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "ingress-1",
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"v=0.6.2",
				"k8sv=v1.27.2",
				"k8s_nodes_count=1",
				"k8s_ingresses_count=1",
				"k8s_pods_count=1",
				"k8s_dataplanes_count=1",
				"controller_dataplane_enabled=true",
				"controller_dataplane_bg_enabled=false",
				"controller_controlplane_enabled=false",
				"controller_gateway_enabled=false",
				"controller_konnect_enabled=true",
				"controller_kongplugininstallation_enabled=false",
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
				&gwtypes.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "control-plane-0",
					},
				},
				&gwtypes.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "control-plane-1",
					},
				},
				&gwtypes.ControlPlane{
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
				"k8s_ingresses_count=0",
				"k8s_dataplanes_count=4",
				"k8s_controlplanes_count=3",
				"k8s_standalone_dataplanes_count=3",
				"k8s_standalone_controlplanes_count=2",
				"controller_dataplane_enabled=true",
				"controller_dataplane_bg_enabled=false",
				"controller_controlplane_enabled=false",
				"controller_gateway_enabled=false",
				"controller_konnect_enabled=true",
				"controller_kongplugininstallation_enabled=false",
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
				"controller_dataplane_enabled=true",
				"controller_dataplane_bg_enabled=false",
				"controller_controlplane_enabled=false",
				"controller_gateway_enabled=false",
				"controller_konnect_enabled=true",
				"controller_kongplugininstallation_enabled=false",
			},
		},
		{
			name: "1 aigateway, 1 dataplane, 1 controlplane",
			objects: []runtime.Object{
				&operatorv1alpha1.AIGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "ai-gateway",
					},
				},
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "ai-gateway-dp",
					},
				},
				&gwtypes.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "ai-gateway-cp",
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"k8s_aigateways_count=0", // NOTE: This does work when run against the cluster.
				"k8s_dataplanes_count=1",
				"k8s_controlplanes_count=1",
				"controller_dataplane_enabled=true",
				"controller_dataplane_bg_enabled=false",
				"controller_controlplane_enabled=false",
				"controller_gateway_enabled=false",
				"controller_konnect_enabled=true",
				"controller_kongplugininstallation_enabled=false",
			},
		},
		{
			name: "Konnect entities",
			objects: []runtime.Object{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "kongservice-1",
					},
				},
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "kongservice-2",
					},
				},
				&configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "kongroute-1",
					},
				},
				&configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "kongconsumer-1",
					},
				},
				&configurationv1beta1.KongConsumerGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "kongconsumergroup-1",
					},
				},
				&configurationv1alpha1.KongSNI{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kong",
						Name:      "kongroute-1",
					},
				},
			},
			expectedReportParts: []string{
				"signal=test-signal",
				"k8s_kongroutes_count=1",
				"k8s_kongservices_count=2",
				"k8s_kongsnis_count=1",
				"k8s_kongconsumers_count=1",
				"k8s_kongconsumergroups_count=1",
				"controller_dataplane_enabled=true",
				"controller_dataplane_bg_enabled=false",
				"controller_controlplane_enabled=false",
				"controller_gateway_enabled=false",
				"controller_konnect_enabled=true",
				"controller_kongplugininstallation_enabled=false",
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := scheme.Get()
			k8sclient := testk8sclient.NewSimpleClientset()

			ctrlClient := prepareControllerClient(scheme, tc.objects...)

			d, ok := k8sclient.Discovery().(*fakediscovery.FakeDiscovery)
			require.True(t, ok)
			d.FakedServerVersion = versionInfo()

			// We need the custom list kinds to prevent:
			// panic: coding error: you must register resource to list kind for every resource you're going
			// to LIST when creating the client.
			// See NewSimpleDynamicClientWithCustomListKinds:
			// https://pkg.go.dev/k8s.io/client-go/dynamic/fake#NewSimpleDynamicClientWithCustomListKinds
			// or register the list into the scheme:
			dyn := testdynclient.NewSimpleDynamicClientWithCustomListKinds(scheme,
				map[schema.GroupVersionResource]string{
					operatorv1alpha1.AIGatewayGVR(): "AIGatewayList",
				},
				tc.objects...,
			)
			meta := metadata.Info{
				Release: "0.6.2",
			}
			cfg := Config{
				DataPlaneControllerEnabled: true,
				KonnectControllerEnabled:   true,
			}

			m, err := createManager(
				types.Signal(SignalPing), k8sclient, ctrlClient, dyn, meta, cfg,
				logr.Discard(),
				telemetry.OptManagerPeriod(time.Hour),
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
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			require.NoError(t, m.TriggerExecute(ctx, "test-signal"), "failed triggering signal execution")

			t.Log("checking received report...")
			requireReportContainsValuesEventually(t, ch, tc.expectedReportParts...)
		})
	}
}

func TestTelemetryUpdates(t *testing.T) {
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
				require.NoError(t, dyn.Resource(operatorv1beta1.DataPlaneGVR()).
					Namespace("kong").
					Delete(ctx, "cloud-gateway-0", metav1.DeleteOptions{}))
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
				_, err := dyn.Resource(operatorv1beta1.DataPlaneGVR()).
					Namespace("kong").
					Create(ctx, &unstructured.Unstructured{
						Object: map[string]any{
							"apiVersion": "gateway-operator.konghq.com/v1beta1",
							"kind":       "DataPlane",
							"metadata": map[string]any{
								"name":      "cloud-gateway-0",
								"namespace": "kong",
							},
						},
					}, metav1.CreateOptions{})
				require.NoError(t, err)
				_, err = dyn.Resource(operatorv1beta1.DataPlaneGVR()).
					Namespace("kong").
					Create(ctx, &unstructured.Unstructured{
						Object: map[string]any{
							"apiVersion": "gateway-operator.konghq.com/v1beta1",
							"kind":       "DataPlane",
							"metadata": map[string]any{
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
				"k8s_nodes_count=0",
				"k8s_pods_count=0",
				// NOTE: For some reason deletions do not work in tests.
				// When we add a custom mapping to NewSimpleDynamicClientWithCustomListKinds:
				//   operatorv1beta1.DataPlaneGVR():  "DataPlaneList",
				// then this works but the previous test case for deletion fails.
				// Surprisingly, this part of the report is not reported here after
				// the update (create actions).
				// "k8s_dataplanes_count=0",
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := scheme.Get()
			// We need the custom list kinds to prevent:
			// panic: coding error: you must register resource to list kind for every resource you're going
			// to LIST when creating the client.
			// See NewSimpleDynamicClientWithCustomListKinds:
			// https://pkg.go.dev/k8s.io/client-go/dynamic/fake#NewSimpleDynamicClientWithCustomListKinds
			// or register the list into the scheme:
			dyn := testdynclient.NewSimpleDynamicClientWithCustomListKinds(
				scheme,
				map[schema.GroupVersionResource]string{
					operatorv1alpha1.AIGatewayGVR(): "AIGatewayList",
				},
				tc.objects...,
			)

			k8sclient := testk8sclient.NewSimpleClientset()
			ctrlClient := prepareControllerClient(scheme)

			dClient := k8sclient.Discovery()
			d, ok := dClient.(*fakediscovery.FakeDiscovery)
			require.True(t, ok)
			d.FakedServerVersion = versionInfo()

			meta := metadata.Info{
				Release: "0.6.2",
			}
			cfg := Config{
				DataPlaneControllerEnabled: true,
			}

			m, err := createManager(
				types.Signal(SignalPing), k8sclient, ctrlClient, dyn, meta, cfg,
				testr.New(t),
				telemetry.OptManagerPeriod(time.Hour),
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
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			require.NoError(t, m.TriggerExecute(ctx, "test-signal"), "failed triggering signal execution")

			t.Log("checking received report...")
			requireReportContainsValuesEventually(t, ch, tc.expectedReportParts...)

			tc.action(t, t.Context(), dyn)

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
				t.Logf("expecting in report: %s", v)
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
