package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v2alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestControlPlaneV2(t *testing.T) {
	validDataPlaneTarget := operatorv2alpha1.ControlPlaneDataPlaneTarget{
		Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
		Ref: &operatorv2alpha1.ControlPlaneDataPlaneTargetRef{
			Name: "dataplane-1",
		},
	}

	validControlPlaneOptions := operatorv2alpha1.ControlPlaneOptions{
		DataPlane: validDataPlaneTarget,
	}

	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2alpha1.ControlPlane]{
			{
				Name: "no extensions",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: validControlPlaneOptions,
					},
				},
			},
			{
				Name: "konnectExtension set",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
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
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
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
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
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

	t.Run("dataplane", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2alpha1.ControlPlane]{
			{
				Name: "missing dataplane causes an error",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.dataplane.type: Unsupported value: \"\""),
			},
			{
				Name: "when dataplane.type is set to name, name must be specified",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Ref has to be provided when type is set to ref"),
			},
			{
				Name: "when dataplane.type is set to ref, external must not be specified",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
								Ref: &operatorv2alpha1.ControlPlaneDataPlaneTargetRef{
									Name: "dataplane-1",
								},
								External: &operatorv2alpha1.ControlPlaneDataPlaneTargetExternal{
									URL: "https://dataplane.example.com:8444/admin",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.dataplane: Invalid value: \"object\": External cannot be provided when type is set to ref"),
			},
			{
				Name: "when dataplane.type is set to external, external.url must be specified",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetExternalType,
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("External has to be provided when type is set to external"),
			},
			{
				Name: "when dataplane.type is set to external, ref cannot be specified",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetExternalType,
								Ref: &operatorv2alpha1.ControlPlaneDataPlaneTargetRef{
									Name: "dataplane-1",
								},
								External: &operatorv2alpha1.ControlPlaneDataPlaneTargetExternal{
									URL: "https://dataplane.example.com:8444/admin",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"object\": Ref cannot be provided when type is set to external"),
			},
			{
				Name: "specifying dataplane ref name when type is ref passes",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
								Ref: &operatorv2alpha1.ControlPlaneDataPlaneTargetRef{
									Name: "dataplane-1",
								},
							},
						},
					},
				},
			},
			{
				Name: "specifying dataplane url when type is external passes: https",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetExternalType,
								External: &operatorv2alpha1.ControlPlaneDataPlaneTargetExternal{
									URL: "https://dataplane.example.com:8444/admin",
								},
							},
						},
					},
				},
			},
			{
				Name: "specifying dataplane url when type is external passes: http, no port",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetExternalType,
								External: &operatorv2alpha1.ControlPlaneDataPlaneTargetExternal{
									URL: "http://dataplane.example.com/admin",
								},
							},
						},
					},
				},
			},
			{
				Name: "dataplane url must be a valid URL, otherwise it fails",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
								Type: operatorv2alpha1.ControlPlaneDataPlaneTargetExternalType,
								External: &operatorv2alpha1.ControlPlaneDataPlaneTargetExternal{
									URL: "not-a-valid-url",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("URL has to be a valid URL"),
			},
		}.Run(t)
	})

	t.Run("feature gates", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2alpha1.ControlPlane]{
			{
				Name: "no feature gates",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: validControlPlaneOptions,
					},
				},
			},
			{
				Name: "feature gate set",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							FeatureGates: []operatorv2alpha1.ControlPlaneFeatureGate{
								{
									Name:  "KongCustomEntity",
									State: operatorv2alpha1.FeatureGateStateEnabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "feature gate disabled",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							FeatureGates: []operatorv2alpha1.ControlPlaneFeatureGate{
								{
									Name:  "KongCustomEntity",
									State: operatorv2alpha1.FeatureGateStateDisabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "feature gate set and then removed",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							FeatureGates: []operatorv2alpha1.ControlPlaneFeatureGate{
								{
									Name:  "KongCustomEntity",
									State: operatorv2alpha1.FeatureGateStateEnabled,
								},
							},
						},
					},
				},
				Update: func(cp *operatorv2alpha1.ControlPlane) {
					cp.Spec.ControlPlaneOptions = validControlPlaneOptions
				},
			},
			{
				Name: "cannot provide a feature gate with enabled unset",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							FeatureGates: []operatorv2alpha1.ControlPlaneFeatureGate{
								{
									Name: "KongCustomEntity",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.featureGates[0].state: Unsupported value: \"\": supported values: \"enabled\", \"disabled\""),
			},
		}.Run(t)
	})

	t.Run("controllers", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2alpha1.ControlPlane]{
			{
				Name: "no controller overrides specified",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: validControlPlaneOptions,
					},
				},
			},
			{
				Name: "controller overrides specified",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							Controllers: []operatorv2alpha1.ControlPlaneController{
								{
									Name:  "GatewayAPI",
									State: operatorv2alpha1.ControllerStateEnabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "controller overrides specified - disabled",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							Controllers: []operatorv2alpha1.ControlPlaneController{
								{
									Name:  "GatewayAPI",
									State: operatorv2alpha1.ControllerStateDisabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "controller overrides specified and then removed",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							Controllers: []operatorv2alpha1.ControlPlaneController{
								{
									Name:  "GatewayAPI",
									State: operatorv2alpha1.ControllerStateEnabled,
								},
							},
						},
					},
				},
				Update: func(cp *operatorv2alpha1.ControlPlane) {
					cp.Spec.ControlPlaneOptions = validControlPlaneOptions
				},
			},
			{
				Name: "cannot provide a controller with enabled unset",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
							DataPlane: validDataPlaneTarget,
							Controllers: []operatorv2alpha1.ControlPlaneController{
								{
									Name: "GatewayAPI",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.controllers[0].state: Unsupported value: \"\": supported values: \"enabled\", \"disabled\""),
			},
		}.Run(t)
	})
}
