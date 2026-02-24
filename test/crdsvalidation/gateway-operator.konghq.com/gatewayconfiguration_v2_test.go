package crdsvalidation_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestGatewayConfigurationV2(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no extensions",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       operatorv2beta1.GatewayConfigurationSpec{},
				},
			},
			{
				Name: "valid konnectExtension at the gatewayConfiguration level",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("spec.extensions: Too many: 3: must have at most 2 items"),
			},
			{
				Name: "invalid konnectExtension",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("Extension not allowed for GatewayConfiguration"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("DataPlaneOptions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no DataPlaneOptions",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying resources.PodDisruptionBudget",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv2beta1.DeploymentOptions{
									Replicas: new(int32(4)),
								},
							},
							Resources: &operatorv2beta1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv2beta1.PodDisruptionBudget{
									Spec: operatorv2beta1.PodDisruptionBudgetSpec{
										MinAvailable:               new(intstr.FromInt(1)),
										UnhealthyPodEvictionPolicy: new(policyv1.IfHealthyBudget),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv2beta1.DeploymentOptions{
									Replicas: new(int32(4)),
								},
							},
							Resources: &operatorv2beta1.GatewayConfigDataPlaneResources{
								PodDisruptionBudget: &operatorv2beta1.PodDisruptionBudget{
									Spec: operatorv2beta1.PodDisruptionBudgetSpec{
										MinAvailable:   new(intstr.FromInt(1)),
										MaxUnavailable: new(intstr.FromInt(1)),
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("You can specify only one of maxUnavailable and minAvailable in a single PodDisruptionBudgetSpec."),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("ControlPlaneOptions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no ControlPlaneOptions",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						ControlPlaneOptions: nil,
					},
				},
			},
			{
				Name: "specifying watch namespaces, type=all",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("Konnect", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "it is valid to specify no Konnect options",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						Konnect: nil,
					},
				},
			},
			{
				Name: "it is valid to specify APIAuthConfigurationRef without Source and Mirror",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						Konnect: &operatorv2beta1.KonnectOptions{
							APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "my-konnect-auth-config",
							},
						},
					},
				},
			},
			{
				Name: "it is valid to specify Mirror field when source is set to Mirror",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						Konnect: &operatorv2beta1.KonnectOptions{
							APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "my-konnect-auth-config",
							},
							Source: new(commonv1alpha1.EntitySourceMirror),
							Mirror: &konnectv1alpha1.MirrorSpec{
								Konnect: konnectv1alpha1.MirrorKonnect{
									ID: commonv1alpha1.KonnectIDType("8ae65120-cdec-4310-84c1-4b19caf67967"),
								},
							},
						},
					},
				},
			},
			{
				Name: "it is valid to have source set to Origin without Mirror field",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						Konnect: &operatorv2beta1.KonnectOptions{
							APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "my-konnect-auth-config",
							},
							Source: new(commonv1alpha1.EntitySourceOrigin),
						},
					},
				},
			},
			{
				Name: "it is invalid to specify Mirror field when source is not set to Mirror",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						Konnect: &operatorv2beta1.KonnectOptions{
							APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "my-konnect-auth-config",
							},
							Source: new(commonv1alpha1.EntitySourceOrigin),
							Mirror: &konnectv1alpha1.MirrorSpec{
								Konnect: konnectv1alpha1.MirrorKonnect{
									ID: commonv1alpha1.KonnectIDType("8ae65120-cdec-4310-84c1-4b19caf67967"),
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("mirror field cannot be set for type Origin"),
			},
			{
				Name: "it is invalid to have source set to Mirror without Mirror field",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						Konnect: &operatorv2beta1.KonnectOptions{
							APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "my-konnect-auth-config",
							},
							Source: new(commonv1alpha1.EntitySourceMirror),
						},
					},
				},
				ExpectedErrorMessage: new("mirror field must be set for type Mirror"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("ListenerOptions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "specify nodeport for listeners with 'NodePort' dataplane ingress service",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("spec.listenersOptions[0].nodePort in body should be greater than or equal to 1"),
			},
			{
				Name: "Cannot specify nodeport for listeners with 'ClusterIP' dataplane ingress service",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("Can only specify listener's NodePort when the type of service for dataplane to receive ingress traffic ('spec.dataPlaneOptions.network.services.ingress') is NodePort or LoadBalancer"),
			},
			{
				Name: "Name must be unique in listener options",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("Listener name must be unique within the Gateway"),
			},
			{
				Name: "Nodeport must be unique in listener options",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("Nodeport must be unique within the Gateway if specified"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("service ingress labels", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.GatewayConfiguration]{
			{
				Name: "can specify service ingress labels",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Network: operatorv2beta1.GatewayConfigDataPlaneNetworkOptions{
								Services: &operatorv2beta1.GatewayConfigDataPlaneServices{
									Ingress: &operatorv2beta1.GatewayConfigServiceOptions{
										ServiceOptions: operatorv2beta1.ServiceOptions{
											Labels: map[string]string{
												"environment": "production",
												"team":        "platform",
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
				Name: "cannot specify service ingress label with value exceeding 63 characters",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Network: operatorv2beta1.GatewayConfigDataPlaneNetworkOptions{
								Services: &operatorv2beta1.GatewayConfigDataPlaneServices{
									Ingress: &operatorv2beta1.GatewayConfigServiceOptions{
										ServiceOptions: operatorv2beta1.ServiceOptions{
											Labels: map[string]string{
												"key": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1",
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("label values must be 63 characters or less"),
			},
			{
				Name: "cannot specify service ingress label with invalid value format",
				TestObject: &operatorv2beta1.GatewayConfiguration{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.GatewayConfigurationSpec{
						DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
							Network: operatorv2beta1.GatewayConfigDataPlaneNetworkOptions{
								Services: &operatorv2beta1.GatewayConfigDataPlaneServices{
									Ingress: &operatorv2beta1.GatewayConfigServiceOptions{
										ServiceOptions: operatorv2beta1.ServiceOptions{
											Labels: map[string]string{
												"key": "-invalid-start",
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("label values must be empty or start and end with an alphanumeric character, with dashes, underscores, and dots in between"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
