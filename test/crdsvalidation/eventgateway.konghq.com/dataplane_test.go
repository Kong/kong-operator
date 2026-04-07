package crdsvalidation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validDataPlane(ns string) *eventgatewayv1alpha1.KegDataPlane {
	return &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
			ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
				Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
					Name: "my-event-gateway",
				},
			},
		},
	}
}

func TestEventGatewayDataPlane(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("controlPlaneRef validation", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.KegDataPlane]{
			{
				Name:       "konnectNamespacedRef type with ref set - valid",
				TestObject: validDataPlane(ns.Name),
			},
			{
				Name: "konnectNamespacedRef type without ref - invalid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
						},
					},
				},
				ExpectedErrorMessage: new("konnectNamespacedRef must be set when type is konnectNamespacedRef"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("service options nodeport validation", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.KegDataPlane]{
			{
				Name:       "no network set",
				TestObject: validDataPlane(ns.Name),
			},
			{
				Name: "kafka service, no ports",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Network: &eventgatewayv1alpha1.NetworkOptions{
							Services: &eventgatewayv1alpha1.Services{
								Kafka: &eventgatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeClusterIP,
								},
							},
						},
					},
				},
			},
			{
				Name: "kafka service, nodePort with type NodePort - valid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Network: &eventgatewayv1alpha1.NetworkOptions{
							Services: &eventgatewayv1alpha1.Services{
								Kafka: &eventgatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeNodePort,
									Ports: []eventgatewayv1alpha1.ServicePort{
										{Port: 9092, NodePort: new(int32(30092))},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "kafka service, nodePort with type LoadBalancer - valid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Network: &eventgatewayv1alpha1.NetworkOptions{
							Services: &eventgatewayv1alpha1.Services{
								Kafka: &eventgatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeLoadBalancer,
									Ports: []eventgatewayv1alpha1.ServicePort{
										{Port: 9092, NodePort: new(int32(30092))},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "kafka service, nodePort with type ClusterIP - invalid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Network: &eventgatewayv1alpha1.NetworkOptions{
							Services: &eventgatewayv1alpha1.Services{
								Kafka: &eventgatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeClusterIP,
									Ports: []eventgatewayv1alpha1.ServicePort{
										{Port: 9092, NodePort: new(int32(30092))},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("Cannot set NodePort when service type is not NodePort or LoadBalancer"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("deployment replicas and scaling are mutually exclusive", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.KegDataPlane]{
			{
				Name: "only replicas set - valid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Deployment: &eventgatewayv1alpha1.DeploymentOptions{
							Replicas: new(int32(2)),
						},
					},
				},
			},
			{
				Name: "only scaling set - valid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Deployment: &eventgatewayv1alpha1.DeploymentOptions{
							Scaling: &eventgatewayv1alpha1.Scaling{
								HorizontalScaling: &eventgatewayv1alpha1.HorizontalScaling{
									MaxReplicas: 5,
								},
							},
						},
					},
				},
			},
			{
				Name: "both replicas and scaling set - invalid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Deployment: &eventgatewayv1alpha1.DeploymentOptions{
							Replicas: new(int32(2)),
							Scaling: &eventgatewayv1alpha1.Scaling{
								HorizontalScaling: &eventgatewayv1alpha1.HorizontalScaling{
									MaxReplicas: 5,
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("Using both replicas and scaling fields is not allowed."),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("observability log flags enum", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.KegDataPlane]{
			{
				Name: "valid log level: info",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
							Observability: &eventgatewayv1alpha1.ObservabilityConfig{
								LogFlags: new("info"),
							},
						},
					},
				},
			},
			{
				Name: "valid log level: debug",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
							Observability: &eventgatewayv1alpha1.ObservabilityConfig{
								LogFlags: new("debug"),
							},
						},
					},
				},
			},
			{
				Name: "invalid log level - invalid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
							Observability: &eventgatewayv1alpha1.ObservabilityConfig{
								LogFlags: new("verbose"),
							},
						},
					},
				},
				ExpectedErrorMessage: new("Unsupported value: \"verbose\""),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("minimum field validations", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.KegDataPlane]{
			{
				Name: "configPollIntervalSeconds=0 - invalid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
							ConfigPollIntervalSeconds: new(int32(0)),
						},
					},
				},
				ExpectedErrorMessage: new("should be greater than or equal to 1"),
			},
			{
				Name: "apiRequestTimeoutSeconds=0 - invalid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
							Konnect: &eventgatewayv1alpha1.KonnectConfig{
								APIRequestTimeoutSeconds: new(int32(0)),
							},
						},
					},
				},
				ExpectedErrorMessage: new("should be greater than or equal to 1"),
			},
			{
				Name: "shutdownTimeoutSeconds=0 - invalid",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
						Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
							Runtime: &eventgatewayv1alpha1.RuntimeOptions{
								ShutdownTimeoutSeconds: new(int32(0)),
							},
						},
					},
				},
				ExpectedErrorMessage: new("should be greater than or equal to 1"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("status defaults", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.KegDataPlane]{
			{
				Name: "ready condition defaulted on first status update",
				TestObject: &eventgatewayv1alpha1.KegDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
						ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
							Type: eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-event-gateway",
							},
						},
					},
					// Non-zero status triggers the framework's status update, which causes
					// the API server to apply the +kubebuilder:default on status.conditions.
					Status: eventgatewayv1alpha1.KegDataPlaneStatus{
						Replicas: 1,
					},
				},
				Assert: func(t *testing.T, dp *eventgatewayv1alpha1.KegDataPlane) {
					var readyCond *metav1.Condition
					for i := range dp.Status.Conditions {
						if dp.Status.Conditions[i].Type == "Ready" {
							readyCond = &dp.Status.Conditions[i]
							break
						}
					}
					require.NotNil(t, readyCond, "Ready condition not found in status")
					assert.Equal(t, metav1.ConditionUnknown, readyCond.Status)
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
