package dataplane

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

func minimalEGDP(ns, name string) *eventgatewayv1alpha1.KegDataPlane {
	return &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
	}
}

// -----------------------------------------------------------------
// generateBaseKafkaService
// -----------------------------------------------------------------

func Test_generateBaseKafkaService(t *testing.T) {
	const (
		ns   = "test-ns"
		name = "my-dp"
	)

	egdp := minimalEGDP(ns, name)
	svc := generateBaseKafkaService(egdp)

	assert.Equal(t, "v1", svc.APIVersion)
	assert.Equal(t, "Service", svc.Kind)
	assert.Equal(t, name+"-kafka", svc.Name)
	assert.Equal(t, ns, svc.Namespace)

	// Selector must target the owning DataPlane.
	assert.Equal(t, consts.DataPlaneManagedByLabelValue, svc.Spec.Selector[consts.GatewayOperatorManagedByLabel])
	assert.Equal(t, name, svc.Spec.Selector[consts.GatewayOperatorManagedByNameLabel])

	// Default port must be the Kafka port.
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, DefaultKafkaPort, svc.Spec.Ports[0].Port)
	assert.Equal(t, intstr.FromInt32(DefaultKafkaPort), svc.Spec.Ports[0].TargetPort)
	assert.Equal(t, corev1.ProtocolTCP, svc.Spec.Ports[0].Protocol)

	// Owner reference must be set.
	require.Len(t, svc.OwnerReferences, 1)
	assert.Equal(t, egdp.Name, svc.OwnerReferences[0].Name)
}

// -----------------------------------------------------------------
// generateKafkaServiceOverlay
// -----------------------------------------------------------------

func Test_generateKafkaServiceOverlay(t *testing.T) {
	tests := []struct {
		name  string
		egdp  *eventgatewayv1alpha1.KegDataPlane
		check func(t *testing.T, svc *corev1.Service)
	}{
		{
			name: "type LoadBalancer propagated",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Network: &eventgatewayv1alpha1.NetworkOptions{
						Services: &eventgatewayv1alpha1.Services{
							Kafka: &eventgatewayv1alpha1.ServiceOptions{
								Type: corev1.ServiceTypeLoadBalancer,
							},
						},
					},
				},
			},
			check: func(t *testing.T, svc *corev1.Service) {
				assert.Equal(t, corev1.ServiceTypeLoadBalancer, svc.Spec.Type)
			},
		},
		{
			name: "labels and annotations propagated",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Network: &eventgatewayv1alpha1.NetworkOptions{
						Services: &eventgatewayv1alpha1.Services{
							Kafka: &eventgatewayv1alpha1.ServiceOptions{
								Labels:      map[eventgatewayv1alpha1.LabelName]eventgatewayv1alpha1.LabelValue{"env": "prod"},
								Annotations: map[string]string{"example.com/key": "val"},
							},
						},
					},
				},
			},
			check: func(t *testing.T, svc *corev1.Service) {
				assert.Equal(t, "prod", svc.Labels["env"])
				assert.Equal(t, "val", svc.Annotations["example.com/key"])
			},
		},
		{
			name: "custom ports mapped correctly",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Network: &eventgatewayv1alpha1.NetworkOptions{
						Services: &eventgatewayv1alpha1.Services{
							Kafka: &eventgatewayv1alpha1.ServiceOptions{
								Ports: []eventgatewayv1alpha1.ServicePort{
									{
										Name:       new("kafka-tls"),
										Port:       9093,
										TargetPort: &intstr.IntOrString{IntVal: 9093, Type: intstr.Int},
										NodePort:   new(int32(30093)),
									},
								},
							},
						},
					},
				},
			},
			check: func(t *testing.T, svc *corev1.Service) {
				require.Len(t, svc.Spec.Ports, 1)
				p := svc.Spec.Ports[0]
				assert.Equal(t, "kafka-tls", p.Name)
				assert.Equal(t, int32(9093), p.Port)
				assert.Equal(t, int32(9093), p.TargetPort.IntVal)
				assert.Equal(t, int32(30093), p.NodePort)
			},
		},
		{
			name: "externalTrafficPolicy propagated",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Network: &eventgatewayv1alpha1.NetworkOptions{
						Services: &eventgatewayv1alpha1.Services{
							Kafka: &eventgatewayv1alpha1.ServiceOptions{
								ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
							},
						},
					},
				},
			},
			check: func(t *testing.T, svc *corev1.Service) {
				assert.Equal(t, corev1.ServiceExternalTrafficPolicyLocal, svc.Spec.ExternalTrafficPolicy)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := generateKafkaServiceOverlay(tc.egdp)
			require.NotNil(t, svc)
			tc.check(t, svc)
		})
	}
}

// -----------------------------------------------------------------
// buildKafkaService
// -----------------------------------------------------------------

func Test_buildKafkaService(t *testing.T) {
	tc := managedfields.NewDeducedTypeConverter()

	tests := []struct {
		name    string
		egdp    *eventgatewayv1alpha1.KegDataPlane
		wantErr bool
		check   func(t *testing.T, obj client.Object)
	}{
		{
			name: "no network spec: returns base service",
			egdp: minimalEGDP("ns", "dp"),
			check: func(t *testing.T, obj client.Object) {
				assert.Equal(t, "dp-kafka", obj.GetName())
			},
		},
		{
			name: "network set but kafka nil: returns base service",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Network: &eventgatewayv1alpha1.NetworkOptions{Services: nil},
				},
			},
			check: func(t *testing.T, obj client.Object) {
				assert.Equal(t, "dp-kafka", obj.GetName())
			},
		},
		{
			name: "kafka service options merged: type propagated",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Network: &eventgatewayv1alpha1.NetworkOptions{
						Services: &eventgatewayv1alpha1.Services{
							Kafka: &eventgatewayv1alpha1.ServiceOptions{
								Type: corev1.ServiceTypeLoadBalancer,
							},
						},
					},
				},
			},
			check: func(t *testing.T, obj client.Object) {
				require.NotNil(t, obj)
				assert.Equal(t, "dp-kafka", obj.GetName())
			},
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			obj, err := buildKafkaService(tc, testcase.egdp)
			if testcase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, obj)
			testcase.check(t, obj)
		})
	}
}

// -----------------------------------------------------------------
// ensureKafkaService
// -----------------------------------------------------------------

func Test_ensureKafkaService(t *testing.T) {
	const (
		ns     = "test-ns"
		dpName = "my-dp"
	)

	tc := managedfields.NewDeducedTypeConverter()
	scheme := managerscheme.Get()

	egdp := &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: dpName},
	}

	tests := []struct {
		name        string
		buildClient func(base client.WithWatch) client.Client
		// prepareRecorder runs before the assertion (e.g. pre-call to set state).
		prepareRecorder func(r *Reconciler, rec *events.FakeRecorder)
		wantErr         bool
		wantEvent       string
	}{
		{
			name:        "first call creates service and records ServiceCreated event",
			buildClient: func(base client.WithWatch) client.Client { return base },
			wantErr:     false,
			wantEvent:   "ServiceCreated",
		},
		{
			name:        "second call after content change records ServiceUpdated event",
			buildClient: func(base client.WithWatch) client.Client { return base },
			prepareRecorder: func(r *Reconciler, rec *events.FakeRecorder) {
				_ = r.ensureKafkaService(context.Background(), logr.Discard(), egdp)
				<-rec.Events
			},
			wantErr:   false,
			wantEvent: "ServiceUpdated",
		},
		{
			name: "Apply error is propagated and ServiceFailed event is recorded",
			buildClient: func(base client.WithWatch) client.Client {
				return interceptor.NewClient(base, interceptor.Funcs{
					Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
						return assert.AnError
					},
				})
			},
			wantErr:   true,
			wantEvent: "ServiceFailed",
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			recorder := events.NewFakeRecorder(10)
			base := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &Reconciler{
				Client:        testcase.buildClient(base),
				typeConverter: tc,
				eventRecorder: recorder,
			}

			if testcase.prepareRecorder != nil {
				testcase.prepareRecorder(r, recorder)
			}

			err := r.ensureKafkaService(context.Background(), logr.Discard(), egdp)

			if testcase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if testcase.wantEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, testcase.wantEvent)
				default:
					t.Errorf("expected event containing %q but channel was empty", testcase.wantEvent)
				}
			} else {
				assert.Empty(t, recorder.Events, "expected no events but got %d", len(recorder.Events))
			}
		})
	}
}
