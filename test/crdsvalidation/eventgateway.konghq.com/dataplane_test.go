package crdsvalidation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validDataPlane(ns string) *eventgatewayv1alpha1.DataPlane {
	return &eventgatewayv1alpha1.DataPlane{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: eventgatewayv1alpha1.DataPlaneSpec{
			KonnectEventGatewayRef: corev1.LocalObjectReference{
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
						KonnectEventGatewayRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
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
						KonnectEventGatewayRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Network: &eventgatewayv1alpha1.NetworkOptions{
							Services: &eventgatewayv1alpha1.Services{
								Kafka: &eventgatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeNodePort,
									Ports: []eventgatewayv1alpha1.ServicePort{
										{Port: 9092, NodePort: 30092},
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
						KonnectEventGatewayRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Network: &eventgatewayv1alpha1.NetworkOptions{
							Services: &eventgatewayv1alpha1.Services{
								Kafka: &eventgatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeLoadBalancer,
									Ports: []eventgatewayv1alpha1.ServicePort{
										{Port: 9092, NodePort: 30092},
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
						KonnectEventGatewayRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Network: &eventgatewayv1alpha1.NetworkOptions{
							Services: &eventgatewayv1alpha1.Services{
								Kafka: &eventgatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeClusterIP,
									Ports: []eventgatewayv1alpha1.ServicePort{
										{Port: 9092, NodePort: 30092},
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

	t.Run("deployment replicas", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.DataPlane]{
			{
				Name: "replicas set to zero",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						KonnectEventGatewayRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Deployment: &eventgatewayv1alpha1.DeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								Replicas: new(int32(0)),
							},
						},
					},
				},
			},
			{
				Name: "replicas set to positive value",
				TestObject: &eventgatewayv1alpha1.DataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: eventgatewayv1alpha1.DataPlaneSpec{
						KonnectEventGatewayRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
						Deployment: &eventgatewayv1alpha1.DeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								Replicas: new(int32(2)),
							},
						},
					},
				},
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
						KonnectEventGatewayRef: corev1.LocalObjectReference{Name: "my-event-gateway"},
					},
					// Non-zero status triggers the framework's status update, which causes
					// the API server to apply the +kubebuilder:default on status.conditions.
					Status: eventgatewayv1alpha1.DataPlaneStatus{
						Replicas: 1,
					},
				},
				// Update runs after the framework's status update+get, capturing t via closure.
				// No spec changes, the noop update is just a vehicle to assert post-status-update state.
				Update: func(dp *eventgatewayv1alpha1.DataPlane) {
					var readyCond *metav1.Condition
					for i := range dp.Status.Conditions {
						if dp.Status.Conditions[i].Type == "Ready" {
							readyCond = &dp.Status.Conditions[i]
							break
						}
					}
					require.NotNil(t, readyCond, "Ready condition not found in status after status update")
					assert.Equal(t, metav1.ConditionUnknown, readyCond.Status)
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
