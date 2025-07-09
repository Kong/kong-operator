package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestGatewayConfiguration(t *testing.T) {
	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.GatewayConfiguration]{
			{
				Name: "no extensions",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec:       operatorv1beta1.GatewayConfigurationSpec{},
				},
			},
			{
				Name: "valid konnectExtension at the gatewayConfiguration level",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
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
			{
				Name: "invalid konnectExtension",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: "wrong.konghq.com",
								Kind:  "wrongExtension",
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "my-konnect-extension",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Extension not allowed for GatewayConfiguration"),
			},
			{
				Name: "konnectExtension at the DataPlane level",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
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
				ExpectedErrorMessage: lo.ToPtr("KonnectExtension must be set at the Gateway level"),
			},
			{
				Name: "konnectExtension at the ControlPlane level",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
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
				ExpectedErrorMessage: lo.ToPtr("KonnectExtension must be set at the Gateway level"),
			},
		}.Run(t)
	})

	t.Run("DataPlaneOptions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.GatewayConfiguration]{
			{
				Name: "no DataPlaneOptions",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						DataPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying resources.PodDisruptionBudget",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									Replicas: lo.ToPtr(int32(4)),
								},
							},
							Resources: &operatorv1beta1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
									Spec: operatorv1beta1.PodDisruptionBudgetSpec{
										MinAvailable:               lo.ToPtr(intstr.FromInt(1)),
										UnhealthyPodEvictionPolicy: lo.ToPtr(policyv1.IfHealthyBudget),
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "specifying resources.PodDisruptionBudget can only specify onf of maxUnavailable and minAvailable",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									Replicas: lo.ToPtr(int32(4)),
								},
							},
							Resources: &operatorv1beta1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
									Spec: operatorv1beta1.PodDisruptionBudgetSpec{
										MinAvailable:   lo.ToPtr(intstr.FromInt(1)),
										MaxUnavailable: lo.ToPtr(intstr.FromInt(1)),
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("You can specify only one of maxUnavailable and minAvailable in a single PodDisruptionBudgetSpec."),
			},
		}.Run(t)
	})

	t.Run("ControlPlaneOptions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.GatewayConfiguration]{
			{
				Name: "no ControlPlaneOptions",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=all",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
							WatchNamespaces: &operatorv1beta1.WatchNamespaces{
								Type: operatorv1beta1.WatchNamespacesTypeAll,
							},
						},
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=own",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
							WatchNamespaces: &operatorv1beta1.WatchNamespaces{
								Type: operatorv1beta1.WatchNamespacesTypeOwn,
							},
						},
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=list",
				TestObject: &operatorv1beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
							WatchNamespaces: &operatorv1beta1.WatchNamespaces{
								Type: operatorv1beta1.WatchNamespacesTypeList,
								List: []string{
									"namespace1",
									"namespace2",
								},
							},
						},
					},
				},
			},
		}.Run(t)
	})
}
