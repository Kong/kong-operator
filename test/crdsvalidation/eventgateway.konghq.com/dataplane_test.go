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

func validDataPlane(ns string) *eventgatewayv1alpha1.DataPlane {
	return &eventgatewayv1alpha1.DataPlane{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: eventgatewayv1alpha1.DataPlaneSpec{
			ControlPlaneRef: corev1.LocalObjectReference{
				Name: "my-event-gateway",
			},
		},
	}
}

func TestEventGatewayDataPlane(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("service options nodeport validation", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.DataPlane]{
			{
				Name:       "no network set",
				TestObject: validDataPlane(ns.Name),
			},
			{
				Name: "kafka service, no ports",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
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
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
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
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
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
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
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
		common.TestCasesGroup[*eventgatewayv1alpha1.DataPlane]{
			{
				Name: "only replicas set - valid",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Deployment: &eventgatewayv1alpha1.DeploymentOptions{
							Replicas: new(int32(2)),
						},
					},
				},
			},
			{
				Name: "only scaling set - valid",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
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
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
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
		common.TestCasesGroup[*eventgatewayv1alpha1.DataPlane]{
			{
				Name: "valid log level: info",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
							Observability: &eventgatewayv1alpha1.ObservabilityConfig{
								LogFlags: new("info"),
							},
						},
					},
				},
			},
			{
				Name: "valid log level: debug",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
							Observability: &eventgatewayv1alpha1.ObservabilityConfig{
								LogFlags: new("debug"),
							},
						},
					},
				},
			},
			{
				Name: "invalid log level - invalid",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
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

	t.Run("spec defaults applied when parent object is set", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.DataPlane]{
			{
				Name: "config defaults applied when config is set",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config:          &eventgatewayv1alpha1.Config{},
					},
				},
				Assert: func(t *testing.T, dp *eventgatewayv1alpha1.DataPlane) {
					require.NotNil(t, dp.Spec.Config)
					require.NotNil(t, dp.Spec.Config.EnableDebugEndpoints)
					assert.Equal(t, eventgatewayv1alpha1.DebugEndpointsStateDisabled, *dp.Spec.Config.EnableDebugEndpoints)
					require.NotNil(t, dp.Spec.Config.ConfigPollIntervalSeconds)
					assert.Equal(t, int32(5), *dp.Spec.Config.ConfigPollIntervalSeconds)
				},
			},
			{
				Name: "konnect config defaults applied when konnect is set",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
							Konnect: &eventgatewayv1alpha1.KonnectConfig{},
						},
					},
				},
				Assert: func(t *testing.T, dp *eventgatewayv1alpha1.DataPlane) {
					require.NotNil(t, dp.Spec.Config.Konnect)
					require.NotNil(t, dp.Spec.Config.Konnect.Domain)
					assert.Equal(t, "konghq.com", *dp.Spec.Config.Konnect.Domain)
					require.NotNil(t, dp.Spec.Config.Konnect.InsecureSkipVerify)
					assert.Equal(t, eventgatewayv1alpha1.TLSVerificationStateDisabled, *dp.Spec.Config.Konnect.InsecureSkipVerify)
					require.NotNil(t, dp.Spec.Config.Konnect.APIRequestTimeoutSeconds)
					assert.Equal(t, int32(5), *dp.Spec.Config.Konnect.APIRequestTimeoutSeconds)
				},
			},
			{
				Name: "observability defaults applied when observability is set",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
							Observability: &eventgatewayv1alpha1.ObservabilityConfig{},
						},
					},
				},
				Assert: func(t *testing.T, dp *eventgatewayv1alpha1.DataPlane) {
					require.NotNil(t, dp.Spec.Config.Observability)
					require.NotNil(t, dp.Spec.Config.Observability.LogFlags)
					assert.Equal(t, "info", *dp.Spec.Config.Observability.LogFlags)
					require.NotNil(t, dp.Spec.Config.Observability.MetricsRollupAllowMap)
					assert.Equal(t, "messaging.operation.name=produce,fetch", *dp.Spec.Config.Observability.MetricsRollupAllowMap)
					require.NotNil(t, dp.Spec.Config.Observability.PolicyErrorsInfoLogIntervalSeconds)
					assert.Equal(t, int32(1), *dp.Spec.Config.Observability.PolicyErrorsInfoLogIntervalSeconds)
				},
			},
			{
				Name: "runtime defaults applied when runtime is set",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
							Runtime: &eventgatewayv1alpha1.RuntimeOptions{},
						},
					},
				},
				Assert: func(t *testing.T, dp *eventgatewayv1alpha1.DataPlane) {
					require.NotNil(t, dp.Spec.Config.Runtime)
					require.NotNil(t, dp.Spec.Config.Runtime.HealthListenerAddressPort)
					assert.Equal(t, "0.0.0.0:8080", *dp.Spec.Config.Runtime.HealthListenerAddressPort)
					require.NotNil(t, dp.Spec.Config.Runtime.DrainDurationSeconds)
					assert.Equal(t, int32(5), *dp.Spec.Config.Runtime.DrainDurationSeconds)
					require.NotNil(t, dp.Spec.Config.Runtime.ShutdownTimeoutSeconds)
					assert.Equal(t, int32(10), *dp.Spec.Config.Runtime.ShutdownTimeoutSeconds)
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("minimum field validations", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.DataPlane]{
			{
				Name: "configPollIntervalSeconds=0 - invalid",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
							ConfigPollIntervalSeconds: new(int32(0)),
						},
					},
				},
				ExpectedErrorMessage: new("should be greater than or equal to 1"),
			},
			{
				Name: "apiRequestTimeoutSeconds=0 - invalid",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
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
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Config: &eventgatewayv1alpha1.Config{
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
		common.TestCasesGroup[*eventgatewayv1alpha1.DataPlane]{
			{
				Name: "ready condition defaulted on first status update",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						ControlPlaneRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
					},
					// Non-zero status triggers the framework's status update, which causes
					// the API server to apply the +kubebuilder:default on status.conditions.
					Status: eventgatewayv1alpha1.DataPlaneStatus{
						Replicas: 1,
					},
				},
				Assert: func(t *testing.T, dp *eventgatewayv1alpha1.DataPlane) {
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
