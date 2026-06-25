package crdsvalidation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validDataPlane(ns string) *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
				Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
					Name: "my-ai-gateway",
				},
			},
		},
	}
}

func TestAIGatewayDataPlane(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("controlPlaneRef validation", func(t *testing.T) {
		common.TestCasesGroup[*aigatewayv1alpha1.AIGatewayDataPlane]{
			{
				Name:       "konnectNamespacedRef type with ref set - valid",
				TestObject: validDataPlane(ns.Name),
			},
			{
				Name: "konnectNamespacedRef type without ref - invalid",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
						},
					},
				},
				ExpectedErrorMessage: new("konnectNamespacedRef must be set when type is konnectNamespacedRef"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("service options nodeport validation", func(t *testing.T) {
		common.TestCasesGroup[*aigatewayv1alpha1.AIGatewayDataPlane]{
			{
				Name:       "no network set",
				TestObject: validDataPlane(ns.Name),
			},
			{
				Name: "ingress service, no ports",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
						Network: &aigatewayv1alpha1.NetworkOptions{
							Services: &aigatewayv1alpha1.Services{
								Ingress: &aigatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeClusterIP,
								},
							},
						},
					},
				},
			},
			{
				Name: "ingress service, nodePort with type NodePort - valid",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
						Network: &aigatewayv1alpha1.NetworkOptions{
							Services: &aigatewayv1alpha1.Services{
								Ingress: &aigatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeNodePort,
									Ports: []aigatewayv1alpha1.ServicePort{
										{Port: 8080, NodePort: new(int32(30080))},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "ingress service, nodePort with type LoadBalancer - valid",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
						Network: &aigatewayv1alpha1.NetworkOptions{
							Services: &aigatewayv1alpha1.Services{
								Ingress: &aigatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeLoadBalancer,
									Ports: []aigatewayv1alpha1.ServicePort{
										{Port: 8080, NodePort: new(int32(30080))},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "ingress service, nodePort with type ClusterIP - invalid",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
						Network: &aigatewayv1alpha1.NetworkOptions{
							Services: &aigatewayv1alpha1.Services{
								Ingress: &aigatewayv1alpha1.ServiceOptions{
									Type: corev1.ServiceTypeClusterIP,
									Ports: []aigatewayv1alpha1.ServicePort{
										{Port: 8080, NodePort: new(int32(30080))},
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
		common.TestCasesGroup[*aigatewayv1alpha1.AIGatewayDataPlane]{
			{
				Name: "only replicas set - valid",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
						Deployment: &aigatewayv1alpha1.DeploymentOptions{
							Replicas: new(int32(2)),
						},
					},
				},
			},
			{
				Name: "only scaling set - valid",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
						Deployment: &aigatewayv1alpha1.DeploymentOptions{
							Scaling: &aigatewayv1alpha1.Scaling{
								HorizontalScaling: &aigatewayv1alpha1.HorizontalScaling{
									MaxReplicas: 5,
								},
							},
						},
					},
				},
			},
			{
				Name: "both replicas and scaling set - invalid",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
						Deployment: &aigatewayv1alpha1.DeploymentOptions{
							Replicas: new(int32(2)),
							Scaling: &aigatewayv1alpha1.Scaling{
								HorizontalScaling: &aigatewayv1alpha1.HorizontalScaling{
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

	t.Run("status defaults", func(t *testing.T) {
		common.TestCasesGroup[*aigatewayv1alpha1.AIGatewayDataPlane]{
			{
				Name: "ready condition defaulted on first status update",
				TestObject: &aigatewayv1alpha1.AIGatewayDataPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
						ControlPlaneRef: aigatewayv1alpha1.ControlPlaneRef{
							Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
							KonnectNamespacedRef: &aigatewayv1alpha1.KonnectNamespacedRef{
								Name: "my-ai-gateway",
							},
						},
					},
					// Non-zero status triggers the framework's status update, which causes
					// the API server to apply the +kubebuilder:default on status.conditions.
					Status: aigatewayv1alpha1.AIGatewayDataPlaneStatus{
						Replicas: 1,
					},
				},
				Assert: func(t *testing.T, dp *aigatewayv1alpha1.AIGatewayDataPlane) {
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
