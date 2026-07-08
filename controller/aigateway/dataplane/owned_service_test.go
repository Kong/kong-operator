package dataplane

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

func minimalAIGWDP(ns, name string) *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
	}
}

// -----------------------------------------------------------------
// generateBaseIngressService
// -----------------------------------------------------------------

func Test_generateBaseIngressService(t *testing.T) {
	const (
		ns   = "test-ns"
		name = "my-dp"
	)

	aigwdp := minimalAIGWDP(ns, name)
	svc := generateBaseIngressService(aigwdp)

	assert.Equal(t, "v1", svc.APIVersion)
	assert.Equal(t, "Service", svc.Kind)
	assert.Equal(t, name+"-ingress", svc.Name)
	assert.Equal(t, ns, svc.Namespace)

	// Selector must target the owning DataPlane.
	assert.Equal(t, consts.AIGatewayDataPlaneManagedByLabelValue, svc.Spec.Selector[consts.GatewayOperatorManagedByLabel])
	assert.Equal(t, name, svc.Spec.Selector[consts.GatewayOperatorManagedByNameLabel])

	// Default port must be the ingress port.
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, DefaultIngressPort, svc.Spec.Ports[0].Port)
	assert.Equal(t, intstr.FromInt32(DefaultIngressPort), svc.Spec.Ports[0].TargetPort)
	assert.Equal(t, corev1.ProtocolTCP, svc.Spec.Ports[0].Protocol)

	// Owner reference must be set.
	require.Len(t, svc.OwnerReferences, 1)
	assert.Equal(t, aigwdp.Name, svc.OwnerReferences[0].Name)
}

// -----------------------------------------------------------------
// generateIngressServiceOverlay
// -----------------------------------------------------------------

func Test_generateIngressServiceOverlay(t *testing.T) {
	tests := []struct {
		name   string
		aigwdp *aigatewayv1alpha1.AIGatewayDataPlane
		check  func(t *testing.T, svc *corev1.Service)
	}{
		{
			name: "type LoadBalancer propagated",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
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
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
								Labels:      map[aigatewayv1alpha1.LabelName]aigatewayv1alpha1.LabelValue{"env": "prod"},
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
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
								Ports: []aigatewayv1alpha1.ServicePort{
									{
										Name:       new("ingress-tls"),
										Port:       8444,
										TargetPort: &intstr.IntOrString{IntVal: 8444, Type: intstr.Int},
										NodePort:   new(int32(30444)),
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
				assert.Equal(t, "ingress-tls", p.Name)
				assert.Equal(t, int32(8444), p.Port)
				assert.Equal(t, int32(8444), p.TargetPort.IntVal)
				assert.Equal(t, int32(30444), p.NodePort)
			},
		},
		{
			name: "externalTrafficPolicy propagated",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
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
		{
			name: "trafficDistribution propagated",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
								TrafficDistribution: new("PreferSameZone"),
							},
						},
					},
				},
			},
			check: func(t *testing.T, svc *corev1.Service) {
				require.NotNil(t, svc.Spec.TrafficDistribution)
				assert.Equal(t, "PreferSameZone", *svc.Spec.TrafficDistribution)
			},
		},
		{
			name: "internalTrafficPolicy propagated",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
								InternalTrafficPolicy: new(corev1.ServiceInternalTrafficPolicyLocal),
							},
						},
					},
				},
			},
			check: func(t *testing.T, svc *corev1.Service) {
				require.NotNil(t, svc.Spec.InternalTrafficPolicy)
				assert.Equal(t, corev1.ServiceInternalTrafficPolicyLocal, *svc.Spec.InternalTrafficPolicy)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := generateIngressServiceOverlay(tc.aigwdp)
			require.NotNil(t, svc)
			tc.check(t, svc)
		})
	}
}

// -----------------------------------------------------------------
// buildIngressService
// -----------------------------------------------------------------

func Test_buildIngressService(t *testing.T) {
	tc := managedfields.NewDeducedTypeConverter()

	tests := []struct {
		name    string
		aigwdp  *aigatewayv1alpha1.AIGatewayDataPlane
		wantErr bool
		check   func(t *testing.T, obj client.Object)
	}{
		{
			name:   "no network spec: returns base service",
			aigwdp: minimalAIGWDP("ns", "dp"),
			check: func(t *testing.T, obj client.Object) {
				assert.Equal(t, "dp-ingress", obj.GetName())
			},
		},
		{
			name: "network set but ingress nil: returns base service",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{Services: nil},
				},
			},
			check: func(t *testing.T, obj client.Object) {
				assert.Equal(t, "dp-ingress", obj.GetName())
			},
		},
		{
			// A user port named "ingress" (same name as the base port) but on a
			// different port number must replace the base port and not produce a
			// Service with two ports that share the same name.
			name: "user port with same name as base port replaces it (no duplicate names)",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
								Ports: []aigatewayv1alpha1.ServicePort{
									{Name: new("ingress"), Port: 18443},
								},
							},
						},
					},
				},
			},
			check: func(t *testing.T, obj client.Object) {
				u, ok := obj.(*unstructured.Unstructured)
				require.True(t, ok)
				ports, _, _ := unstructured.NestedSlice(u.Object, "spec", "ports")
				var ingressPorts []any
				for _, p := range ports {
					pm, ok := p.(map[string]any)
					if ok && pm["name"] == "ingress" {
						ingressPorts = append(ingressPorts, pm)
					}
				}
				require.Len(t, ingressPorts, 1, "expected exactly one port named 'ingress'")
				portNum, _, _ := unstructured.NestedInt64(ingressPorts[0].(map[string]any), "port")
				assert.Equal(t, int64(18443), portNum)
			},
		},
		{
			name: "ingress service options merged: type propagated",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "dp"},
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Network: &aigatewayv1alpha1.NetworkOptions{
						Services: &aigatewayv1alpha1.Services{
							Ingress: &aigatewayv1alpha1.ServiceOptions{
								Type: corev1.ServiceTypeLoadBalancer,
							},
						},
					},
				},
			},
			check: func(t *testing.T, obj client.Object) {
				require.NotNil(t, obj)
				assert.Equal(t, "dp-ingress", obj.GetName())
			},
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			obj, err := buildIngressService(tc, testcase.aigwdp)
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
// ensureIngressService
// -----------------------------------------------------------------

func Test_ensureIngressService(t *testing.T) {
	const (
		ns     = "test-ns"
		dpName = "my-dp"
	)

	tc := managedfields.NewDeducedTypeConverter()
	scheme := managerscheme.Get()

	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
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
				_ = r.ensureIngressService(context.Background(), logr.Discard(), aigwdp)
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
				TypeConverter: tc,
				eventRecorder: recorder,
			}

			if testcase.prepareRecorder != nil {
				testcase.prepareRecorder(r, recorder)
			}

			err := r.ensureIngressService(context.Background(), logr.Discard(), aigwdp)

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
