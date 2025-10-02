package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/test/crdsvalidation/common"
)

func TestGatewayConfigurationV2(t *testing.T) {
	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no extensions",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec:       operatorv2beta1.GatewayConfigurationSpec{},
				},
			},
			{
				Name: "valid konnectExtension at the gatewayConfiguration level",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
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
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
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
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
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
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
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
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
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
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no DataPlaneOptions",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying resources.PodDisruptionBudget",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv2beta1.DeploymentOptions{
									Replicas: lo.ToPtr(int32(4)),
								},
							},
							Resources: &operatorv2beta1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv2beta1.PodDisruptionBudget{
									Spec: operatorv2beta1.PodDisruptionBudgetSpec{
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
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv2beta1.DeploymentOptions{
									Replicas: lo.ToPtr(int32(4)),
								},
							},
							Resources: &operatorv2beta1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv2beta1.PodDisruptionBudget{
									Spec: operatorv2beta1.PodDisruptionBudgetSpec{
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
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no ControlPlaneOptions",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=all",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv2beta1.GatewayConfigControlPlaneOptions{
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								WatchNamespaces: &operatorv2beta1.WatchNamespaces{
									Type: operatorv2beta1.WatchNamespacesTypeAll,
								},
							},
						},
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=own",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv2beta1.GatewayConfigControlPlaneOptions{
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								WatchNamespaces: &operatorv2beta1.WatchNamespaces{
									Type: operatorv2beta1.WatchNamespacesTypeOwn,
								},
							},
						},
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=list",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: &operatorv2beta1.GatewayConfigControlPlaneOptions{
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								WatchNamespaces: &operatorv2beta1.WatchNamespaces{
									Type: operatorv2beta1.WatchNamespacesTypeList,
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
		}.Run(t)
	})

	t.Run("ListenerOptions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "specify nodeport for listeners with 'NodePort' dataplane ingress service",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Network: operatorv2beta1.GatewayConfigDataPlaneNetworkOptions{
								Services: &operatorv2beta1.GatewayConfigDataPlaneServices{
									Ingress: &operatorv2beta1.GatewayConfigServiceOptions{
										ServiceOptions: operatorv2beta1.ServiceOptions{
											Type: corev1.ServiceTypeNodePort,
										},
									},
								},
							},
						},
						ListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
							{
								Name:     "http",
								NodePort: int32(30080),
							},
						},
					},
				},
			},
			{
				Name: "nodePort out of range",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Network: operatorv2beta1.GatewayConfigDataPlaneNetworkOptions{
								Services: &operatorv2beta1.GatewayConfigDataPlaneServices{
									Ingress: &operatorv2beta1.GatewayConfigServiceOptions{
										ServiceOptions: operatorv2beta1.ServiceOptions{
											Type: corev1.ServiceTypeNodePort,
										},
									},
								},
							},
						},
						ListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
							{
								Name:     "http",
								NodePort: int32(0),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.listenersOptions[0].nodePort in body should be greater than or equal to 1"),
			},
			{
				Name: "Cannot specify nodeport for listeners with 'ClusterIP' dataplane ingress service",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Network: operatorv2beta1.GatewayConfigDataPlaneNetworkOptions{
								Services: &operatorv2beta1.GatewayConfigDataPlaneServices{
									Ingress: &operatorv2beta1.GatewayConfigServiceOptions{
										ServiceOptions: operatorv2beta1.ServiceOptions{
											Type: corev1.ServiceTypeClusterIP,
										},
									},
								},
							},
						},
						ListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
							{
								Name:     "http",
								NodePort: int32(30080),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Can only specify listener's NodePort when the type of service for dataplane to receive ingress traffic ('spec.dataPlaneOptions.network.services.ingress') is NodePort or LoadBalancer"),
			},
			{
				Name: "Name must be unique in listener options",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						ListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
							{
								Name:     "http",
								NodePort: int32(30080),
							},
							{
								Name:     "http",
								NodePort: int32(30081),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Listener name must be unique within the Gateway"),
			},
			{
				Name: "Nodeport must be unique in listener options",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						ListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
							{
								Name:     "http",
								NodePort: int32(30080),
							},
							{
								Name:     "http-1",
								NodePort: int32(30080),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Nodeport must be unique within the Gateway if specified"),
			},
		}.Run(t)
	})
}
