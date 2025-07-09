package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2alpha1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestControlPlaneV2(t *testing.T) {
	validDataPlaneTarget := operatorv2alpha1.ControlPlaneDataPlaneTarget{
		Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
		Ref: &operatorv2alpha1.ControlPlaneDataPlaneTargetRef{
			Name: "dataplane-1",
		},
	}

	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2alpha1.ControlPlane]{
			{
				Name: "no extensions",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane:           validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
					},
				},
			},
			{
				Name: "konnectExtension set",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane:           validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
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
				Name: "konnectExtension and DataPlaneMetricsExtension set",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane:           validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
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
			{
				Name: "invalid extension",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane:           validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
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
						DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
							Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
						},
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Ref has to be provided when type is set to ref"),
			},
			{
				Name: "specifying dataplane ref name when type is ref passes",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
							Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
							Ref: &operatorv2alpha1.ControlPlaneDataPlaneTargetRef{
								Name: "dataplane-1",
							},
						},
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
					},
				},
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
						DataPlane:           validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
					},
				},
			},
			{
				Name: "feature gate set",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
					cp.Spec.ControlPlaneOptions = operatorv2alpha1.ControlPlaneOptions{}
				},
			},
			{
				Name: "cannot provide a feature gate with enabled unset",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
						DataPlane:           validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{},
					},
				},
			},
			{
				Name: "controller overrides specified",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
					cp.Spec.ControlPlaneOptions = operatorv2alpha1.ControlPlaneOptions{}
				},
			},
			{
				Name: "cannot provide a controller with enabled unset",
				TestObject: &operatorv2alpha1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv2alpha1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
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
