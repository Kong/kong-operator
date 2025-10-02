package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	"github.com/kong/kong-operator/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
)

func TestDataplane(t *testing.T) {
	validDataplaneOptions := operatorv1beta1.DataPlaneOptions{
		Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
			DeploymentOptions: operatorv1beta1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "proxy",
								Image: "kong:over9000",
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
	}
	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "no extensions",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: validDataplaneOptions,
					},
				},
			},
			{
				Name: "konnectExtension set",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Extensions: []commonv1alpha1.ExtensionRef{
								{
									Group: "konnect.konghq.com",
									Kind:  "KonnectExtension",
									NamespacedRef: commonv1alpha1.NamespacedRef{
										Name: "my-konnect-extension",
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "invalid extension",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Extensions: []commonv1alpha1.ExtensionRef{
								{
									Group: "invalid.konghq.com",
									Kind:  "KonnectExtension",
									NamespacedRef: commonv1alpha1.NamespacedRef{
										Name: "my-konnect-extension",
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Extension not allowed for DataPlane"),
			},
		}.Run(t)
	})
	t.Run("pod spec", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "no deploymentSpec",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec:       operatorv1beta1.DataPlaneSpec{},
				},
				ExpectedErrorMessage: lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "with deploymentSpec",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "missing container",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "my-container",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "proxy container, no image",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "proxy",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "proxy container, image",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:over9000",
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
		}.Run(t)
	})
	t.Run("db mode", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "db mode on",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:over9000",
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_DATABASE",
															Value: "on",
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
				Name: "db mode off",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: validDataplaneOptions,
					},
				},
			},
		}.Run(t)
	})
	t.Run("service options", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "nodePort can be specified when service type is set to NodePort",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Type: corev1.ServiceTypeNodePort,
										},
										Ports: []operatorv1beta1.DataPlaneServicePort{
											{
												Name:       "http",
												Port:       80,
												NodePort:   30080,
												TargetPort: intstr.FromInt(80),
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
				Name: "can leave nodePort empty when when service type is not specified",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										Ports: []operatorv1beta1.DataPlaneServicePort{
											{
												Name:       "http",
												Port:       80,
												TargetPort: intstr.FromInt(80),
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
				Name: "nodePort can be specified when service type is set to LoadBalancer",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Type: corev1.ServiceTypeLoadBalancer,
										},
										Ports: []operatorv1beta1.DataPlaneServicePort{
											{
												Name:       "http",
												Port:       80,
												NodePort:   30080,
												TargetPort: intstr.FromInt(80),
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
				Name: "cannot specify nodePort when service type is ClusterIP",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Type: corev1.ServiceTypeClusterIP,
										},
										Ports: []operatorv1beta1.DataPlaneServicePort{
											{
												Name:       "http",
												Port:       80,
												NodePort:   30080,
												TargetPort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Cannot set NodePort when service type is not NodePort or LoadBalancer"),
			},
			{
				Name: "can specify nodePort when service type is not set (default LoadBalancer)",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										Ports: []operatorv1beta1.DataPlaneServicePort{
											{
												Name:       "http",
												Port:       80,
												NodePort:   30080,
												TargetPort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}.Run(t)
	})
	t.Run("service ingress type", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "service ingress type LoadBalancer",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Type: corev1.ServiceTypeLoadBalancer,
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "service ingress type NodePort",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Type: corev1.ServiceTypeNodePort,
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "service ingress type ClusterIP",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Type: corev1.ServiceTypeClusterIP,
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "service ingress type ExternalName",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Type: corev1.ServiceTypeExternalName,
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Unsupported value: \"ExternalName\""),
			},
		}.Run(t)
	})

	t.Run("spec update", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "cannot update spec when in the middle of promotion",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
						},
					},
					Status: operatorv1beta1.DataPlaneStatus{
						RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
							Conditions: []metav1.Condition{
								{
									Type:               string(dataplane.DataPlaneConditionTypeRolledOut),
									Status:             metav1.ConditionFalse,
									Reason:             string(dataplane.DataPlaneConditionReasonRolloutPromotionInProgress),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				Update: func(d *operatorv1beta1.DataPlane) {
					d.Spec.Deployment.PodTemplateSpec.Labels = map[string]string{"foo": "bar"}
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("DataPlane spec cannot be updated when promotion is in progress"),
			},
			{
				Name: "rollout status without conditions doesn't prevent spec updates",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
						},
					},
					Status: operatorv1beta1.DataPlaneStatus{
						RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
							Services: &operatorv1beta1.DataPlaneRolloutStatusServices{
								Ingress: &operatorv1beta1.RolloutStatusService{
									Name: "ingress",
									Addresses: []operatorv1beta1.Address{
										{
											Type:       lo.ToPtr(operatorv1beta1.IPAddressType),
											Value:      "10.1.2.3",
											SourceType: operatorv1beta1.PublicIPAddressSourceType,
										},
									},
								},
							},
						},
					},
				},
				Update: func(d *operatorv1beta1.DataPlane) {
					d.Spec.Deployment.PodTemplateSpec.Labels = map[string]string{"foo": "bar"}
				},
			},
			{
				Name: "can update spec when promotion complete",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
						},
					},
					Status: operatorv1beta1.DataPlaneStatus{
						RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
							Conditions: []metav1.Condition{
								{
									Type:               string(dataplane.DataPlaneConditionTypeRolledOut),
									Status:             metav1.ConditionTrue,
									Reason:             string(dataplane.DataPlaneConditionReasonRolloutPromotionDone),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				Update: func(d *operatorv1beta1.DataPlane) {
					d.Spec.Deployment.PodTemplateSpec.Labels = map[string]string{"foo": "bar"}
				},
			},
			{
				Name: "can update spec when rollout is not in progress",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: validDataplaneOptions.Deployment,
						},
					},
				},
				Update: func(d *operatorv1beta1.DataPlane) {
					d.Spec.Deployment.PodTemplateSpec.Labels = map[string]string{"foo": "bar"}
				},
			},
		}.Run(t)
	})
}
