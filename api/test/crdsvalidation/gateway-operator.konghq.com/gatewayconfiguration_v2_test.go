package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v2alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestGatewayConfigurationV2(t *testing.T) {
	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2alpha1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no extensions",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec:       operatorv2alpha1.GatewayConfigurationSpec{},
				},
			},
			{
				Name: "valid konnectExtension at the gatewayConfiguration level",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
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
				Name: "valid DataPlaneMetricsExtension at the gatewayConfiguration level",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: "gateway-operator.konghq.com",
								Kind:  "DataPlaneMetricsExtension",
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "my-dataplane-metrics-extension",
								},
							},
						},
					},
				},
			},
			{
				Name: "valid DataPlaneMetricsExtension and KonnectExtension at the gatewayConfiguration level",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: "gateway-operator.konghq.com",
								Kind:  "DataPlaneMetricsExtension",
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "my-dataplane-metrics-extension",
								},
							},
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
				Name: "invalid 3 extensions (max 2 are allowed) at the gatewayConfiguration level",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: "gateway-operator.konghq.com",
								Kind:  "DataPlaneMetricsExtension",
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "my-dataplane-metrics-extension",
								},
							},
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectExtension",
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "my-konnect-extension",
								},
							},
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectExtension",
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "my-konnect-extension-2",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.extensions: Too many: 3: must have at most 2 items"),
			},
			{
				Name: "invalid konnectExtension",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
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
		}.Run(t)
	})

	t.Run("DataPlaneOptions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2alpha1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no DataPlaneOptions",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						DataPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying resources.PodDisruptionBudget",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2alpha1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									Replicas: lo.ToPtr(int32(4)),
								},
							},
							Resources: &operatorv2alpha1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv2alpha1.PodDisruptionBudget{
									Spec: operatorv2alpha1.PodDisruptionBudgetSpec{
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
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2alpha1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									Replicas: lo.ToPtr(int32(4)),
								},
							},
							Resources: &operatorv2alpha1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv2alpha1.PodDisruptionBudget{
									Spec: operatorv2alpha1.PodDisruptionBudgetSpec{
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
		common.TestCasesGroup[*operatorv2alpha1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no ControlPlaneOptions",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						ControlPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=all",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv2alpha1.GatewayConfigControlPlaneOptions{
							ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
								WatchNamespaces: &operatorv1beta1.WatchNamespaces{
									Type: operatorv1beta1.WatchNamespacesTypeAll,
								},
							},
						},
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=own",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv2alpha1.GatewayConfigControlPlaneOptions{
							ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
								WatchNamespaces: &operatorv1beta1.WatchNamespaces{
									Type: operatorv1beta1.WatchNamespacesTypeOwn,
								},
							},
						},
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=list",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv2alpha1.GatewayConfigControlPlaneOptions{
							ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
			},
			{
				Name: "specifying Admin API workspace",
				TestObject: &operatorv2alpha1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv2alpha1.GatewayConfigControlPlaneOptions{
							ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
								AdminAPI: &operatorv2alpha1.ControlPlaneAdminAPI{
									Workspace: "myworkspacename",
								},
							},
						},
					},
				},
			},
		}.Run(t)
	})
}
