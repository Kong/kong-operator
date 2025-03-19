package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
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
				ExpectedErrorMessage: lo.ToPtr(" DataPlane supports only db mode 'off'"),
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
}
