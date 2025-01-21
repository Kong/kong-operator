package crdsvalidation

import (
	"context"
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/test/envtest"

	kcfgcrdsvalidation "github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

func TestDataPlane(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	t.Run("spec", func(t *testing.T) {
		kcfgcrdsvalidation.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "not providing image fails",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
					Spec: operatorv1beta1.DataPlaneSpec{},
				},
				ExpectedErrorMessage: lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "providing image succeeds",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
			},
			{
				Name: "dbmode '' is supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
															Value: "",
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
			},
			{
				Name: "dbmode off is supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
			},
			{
				Name: "dbmode postgres is not supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
				ExpectedErrorMessage: lo.ToPtr("DataPlane supports only db mode 'off'"),
			},
			{
				Name: "can't update DataPlane when rollout in progress",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
									Reason:             string(consts.DataPlaneConditionReasonRolloutPromotionInProgress),
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
				ExpectedUpdateErrorMessage: lo.ToPtr("DataPlane spec cannot be updated when promotion is in progress"),
			},
			{
				Name: "can update DataPlane when rollout not in progress",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
									Reason:             string(consts.DataPlaneConditionReasonRolloutWaitingForChange),
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
			},
			{
				Name: "BlueGreen promotion strategy AutomaticPromotion is not supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
				ExpectedErrorMessage: lo.ToPtr("Unsupported value: \"AutomaticPromotion\": supported values: \"BreakBeforePromotion\""),
			},
			{
				Name: "BlueGreen promotion strategy BreakBeforePromotion is supported",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
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
												Strategy: operatorv1beta1.BreakBeforePromotion,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme.Get())
	})
}
