package crdsvalidation

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/envtest"

	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestDataPlane(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	commonObjectMeta := metav1.ObjectMeta{
		GenerateName: "dp-",
		Namespace:    ns.Name,
	}

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "not providing image fails",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec:       operatorv1beta1.DataPlaneSpec{},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedErrorMessage:          lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "providing image succeeds",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
			{
				Name: "dbmode off is supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_DATABASE",
															Value: "off",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
			{
				Name: "dbmode postgres is not supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_DATABASE",
															Value: "postgres",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedErrorMessage:          lo.ToPtr("DataPlane supports only db mode 'off'"),
			},
			{
				Name: "can't update DataPlane when rollout in progress",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
												},
											},
										},
									},
								},
							},
						},
					},
					Status: operatorv1beta1.DataPlaneStatus{
						RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
							Conditions: []metav1.Condition{
								{
									Type:               "RolledOut",
									Status:             "True",
									Reason:             string(kcfgdataplane.DataPlaneConditionReasonRolloutPromotionInProgress),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				Update: func(obj *operatorv1beta1.DataPlane) {
					obj.Spec.DataPlaneOptions.Network.Services = &operatorv1beta1.DataPlaneServices{
						Ingress: &operatorv1beta1.DataPlaneServiceOptions{
							ServiceOptions: operatorv1beta1.ServiceOptions{
								Type: corev1.ServiceTypeClusterIP,
							},
						},
					}
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedUpdateErrorMessage:    lo.ToPtr("DataPlane spec cannot be updated when promotion is in progress"),
			},
			{
				Name: "can update DataPlane when rollout not in progress",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
												},
											},
										},
									},
								},
							},
						},
					},
					Status: operatorv1beta1.DataPlaneStatus{
						RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
							Conditions: []metav1.Condition{
								{
									Type:               "RolledOut",
									Status:             "True",
									Reason:             string(kcfgdataplane.DataPlaneConditionReasonRolloutWaitingForChange),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				Update: func(obj *operatorv1beta1.DataPlane) {
					obj.Spec.DataPlaneOptions.Network.Services = &operatorv1beta1.DataPlaneServices{
						Ingress: &operatorv1beta1.DataPlaneServiceOptions{
							ServiceOptions: operatorv1beta1.ServiceOptions{
								Type: corev1.ServiceTypeClusterIP,
							},
						},
					}
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
			{
				Name: "BlueGreen promotion strategy AutomaticPromotion is not supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_DATABASE",
															Value: "postgres",
														},
													},
												},
											},
										},
									},
								},
								Rollout: &operatorv1beta1.Rollout{
									Strategy: operatorv1beta1.RolloutStrategy{
										BlueGreen: &operatorv1beta1.BlueGreenStrategy{
											Promotion: operatorv1beta1.Promotion{
												Strategy: operatorv1beta1.AutomaticPromotion,
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedErrorMessage:          lo.ToPtr("Unsupported value: \"AutomaticPromotion\": supported values: \"BreakBeforePromotion\""),
			},
			{
				Name: "BlueGreen promotion strategy BreakBeforePromotion is supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
												},
											},
										},
									},
								},
								Rollout: &operatorv1beta1.Rollout{
									Strategy: operatorv1beta1.RolloutStrategy{
										BlueGreen: &operatorv1beta1.BlueGreenStrategy{
											Promotion: operatorv1beta1.Promotion{
												Strategy: operatorv1beta1.BreakBeforePromotion,
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
			{
				Name: "BlueGreen rollout resource plan DeleteOnPromotionRecreateOnRollout in unsupported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
												},
											},
										},
									},
								},
								Rollout: &operatorv1beta1.Rollout{
									Strategy: operatorv1beta1.RolloutStrategy{
										BlueGreen: &operatorv1beta1.BlueGreenStrategy{
											Promotion: operatorv1beta1.Promotion{
												Strategy: operatorv1beta1.BreakBeforePromotion,
											},
											Resources: operatorv1beta1.RolloutResources{
												Plan: operatorv1beta1.RolloutResourcePlan{
													Deployment: operatorv1beta1.RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout,
												},
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedErrorMessage:          lo.ToPtr("spec.deployment.rollout.strategy.blueGreen.resources.plan.deployment: Unsupported value: \"DeleteOnPromotionRecreateOnRollout\": supported values: \"ScaleDownOnPromotionScaleUpOnRollout\""),
			},
			{
				Name: "BlueGreen rollout resource plan ScaleDownOnPromotionScaleUpOnRollout in supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
												},
											},
										},
									},
								},
								Rollout: &operatorv1beta1.Rollout{
									Strategy: operatorv1beta1.RolloutStrategy{
										BlueGreen: &operatorv1beta1.BlueGreenStrategy{
											Promotion: operatorv1beta1.Promotion{
												Strategy: operatorv1beta1.BreakBeforePromotion,
											},
											Resources: operatorv1beta1.RolloutResources{
												Plan: operatorv1beta1.RolloutResourcePlan{
													Deployment: operatorv1beta1.RolloutResourcePlanDeploymentScaleDownOnPromotionScaleUpOnRollout,
												},
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
		}.RunWithConfig(t, cfg, scheme.Get())
	})
}
