package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestControlPlane(t *testing.T) {
	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.ControlPlane]{
			{
				Name: "no extensions",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec:       operatorv1beta1.ControlPlaneSpec{},
				},
			},
			{
				Name: "konnectExtension set",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
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
				Name: "konnectExtension and DataPlaneMetricsExtension set",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
							Extensions: []commonv1alpha1.ExtensionRef{
								{
									Group: "konnect.konghq.com",
									Kind:  "KonnectExtension",
									NamespacedRef: commonv1alpha1.NamespacedRef{
										Name: "my-konnect-extension",
									},
								},
								{
									Group: "gateway-operator.konghq.com",
									Kind:  "DataPlaneMetricsExtension",
									NamespacedRef: commonv1alpha1.NamespacedRef{
										Name: "my-metrics-extension",
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "invalid extension",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
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
				ExpectedErrorMessage: lo.ToPtr("Extension not allowed for ControlPlane"),
			},
		}.Run(t)
	})
	t.Run("pod spec", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.ControlPlane]{
			{
				Name: "no deploymentSpec",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec:       operatorv1beta1.ControlPlaneSpec{},
				},
			},
			{
				Name: "with deploymentSpec",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
							Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{},
						},
					},
				},
			},
			{
				Name: "missing container",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
							Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
				ExpectedErrorMessage: lo.ToPtr("ControlPlane requires an image to be set on controller container"),
			},
			{
				Name: "controller container, no image",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
							Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: "controller",
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("ControlPlane requires an image to be set on controller container"),
			},
			{
				Name: "controller container, image",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
							Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  "controller",
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
		}.Run(t)
	})
}
