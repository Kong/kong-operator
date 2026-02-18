package crdsvalidation_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestControlPlaneV2(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	validDataPlaneTarget := operatorv2beta1.ControlPlaneDataPlaneTarget{
		Type: operatorv2beta1.ControlPlaneDataPlaneTargetRefType,
		Ref: &operatorv2beta1.ControlPlaneDataPlaneTargetRef{
			Name: "dataplane-1",
		},
	}

	t.Run("extensions", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
			{
				Name: "no extensions",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
			},
			{
				Name: "konnectExtension set",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
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
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
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
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
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
				ExpectedErrorMessage: new("Extension not allowed for ControlPlane"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("dataplane", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
			{
				Name: "missing dataplane causes an error",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
				ExpectedErrorMessage: new("spec.dataplane: Required value, <nil>: Invalid value:"),
			},
			{
				Name: "when dataplane.type is set to name, name must be specified",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
							Type: operatorv2beta1.ControlPlaneDataPlaneTargetRefType,
						},
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
				ExpectedErrorMessage: new("Ref has to be provided when type is set to ref"),
			},
			{
				Name: "specifying dataplane ref name when type is ref passes",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
							Type: operatorv2beta1.ControlPlaneDataPlaneTargetRefType,
							Ref: &operatorv2beta1.ControlPlaneDataPlaneTargetRef{
								Name: "dataplane-1",
							},
						},
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
			},
			{
				// NOTE: used by operator's Gateway controller
				Name: "managedByOwner is allowed and doesn't require ingressClass to be set",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
							Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
						},
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{},
					},
				},
			},
			{
				// NOTE: used by operator's Gateway controller
				Name: "managedByOwner is allowed and ingressClass can be set",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
							Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
						},
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
			},
			{
				// NOTE: used by operator's Gateway controller
				Name: "can't set ref when type is managedByOwner",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
							Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
							Ref: &operatorv2beta1.ControlPlaneDataPlaneTargetRef{
								Name: "dataplane-1",
							},
						},
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
				ExpectedErrorMessage: new("Ref cannot be provided when type is set to managedByOwner"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("feature gates", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
			{
				Name: "no feature gates",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
			},
			{
				Name: "feature gate set",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
								{
									Name:  "KongCustomEntity",
									State: operatorv2beta1.FeatureGateStateEnabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "feature gate disabled",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
								{
									Name:  "KongCustomEntity",
									State: operatorv2beta1.FeatureGateStateDisabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "feature gate set and then removed",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
								{
									Name:  "KongCustomEntity",
									State: operatorv2beta1.FeatureGateStateEnabled,
								},
							},
						},
					},
				},
				Update: func(cp *operatorv2beta1.ControlPlane) {
					cp.Spec.ControlPlaneOptions = operatorv2beta1.ControlPlaneOptions{
						IngressClass: new("kong"),
					}
				},
			},
			{
				Name: "cannot provide a feature gate with enabled unset",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
								{
									Name: "KongCustomEntity",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("spec.featureGates[0].state: Unsupported value: \"\": supported values: \"enabled\", \"disabled\""),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("controllers", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
			{
				Name: "no controller overrides specified",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
			},
			{
				Name: "controller overrides specified",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Controllers: []operatorv2beta1.ControlPlaneController{
								{
									Name:  "KONG_PLUGIN",
									State: operatorv2beta1.ControllerStateEnabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "controller overrides specified - disabled",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Controllers: []operatorv2beta1.ControlPlaneController{
								{
									Name:  "KONG_PLUGIN",
									State: operatorv2beta1.ControllerStateDisabled,
								},
							},
						},
					},
				},
			},
			{
				Name: "controller overrides specified and then removed",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Controllers: []operatorv2beta1.ControlPlaneController{
								{
									Name:  "KONG_PLUGIN",
									State: operatorv2beta1.ControllerStateEnabled,
								},
							},
						},
					},
				},
				Update: func(cp *operatorv2beta1.ControlPlane) {
					cp.Spec.ControlPlaneOptions = operatorv2beta1.ControlPlaneOptions{
						IngressClass: new("kong"),
					}
				},
			},
			{
				Name: "cannot provide a controller with enabled unset",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Controllers: []operatorv2beta1.ControlPlaneController{
								{
									Name: "KONG_PLUGIN",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("spec.controllers[0].state: Unsupported value: \"\": supported values: \"enabled\", \"disabled\""),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("translation", func(t *testing.T) {
		common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
			{
				Name: "combinedServicesFromDifferentHTTPRoutes set to enabled",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								CombinedServicesFromDifferentHTTPRoutes: new(operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled),
							},
						},
					},
				},
			},
			{
				Name: "combinedServicesFromDifferentHTTPRoutes are set to enabled by default",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
						},
					},
				},
				Assert: func(t *testing.T, cp *operatorv2beta1.ControlPlane) {
					require.NotNil(t, cp.Spec.Translation)
					require.NotNil(t, cp.Spec.Translation.CombinedServicesFromDifferentHTTPRoutes)
					require.Equal(t,
						operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled,
						*cp.Spec.Translation.CombinedServicesFromDifferentHTTPRoutes,
					)
				},
			},
			{
				Name: "combinedServicesFromDifferentHTTPRoutes set to disabled",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								CombinedServicesFromDifferentHTTPRoutes: new(operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled),
							},
						},
					},
				},
			},
			{
				Name: "combinedServicesFromDifferentHTTPRoutes set to disallowed value",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								CombinedServicesFromDifferentHTTPRoutes: new(operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesState("invalid")),
							},
						},
					},
				},
				ExpectedErrorMessage: new("spec.translation.combinedServicesFromDifferentHTTPRoutes: Unsupported value: \"invalid\": supported values: \"enabled\", \"disabled\""),
			},
			{
				Name: "drainSupport set to enabled",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								DrainSupport: new(operatorv2beta1.ControlPlaneDrainSupportStateEnabled),
							},
						},
					},
				},
			},
			{
				Name: "drainSupport set to disabled",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								DrainSupport: new(operatorv2beta1.ControlPlaneDrainSupportStateDisabled),
							},
						},
					},
				},
			},
			{
				Name: "drainSupport set to disallowed value",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								DrainSupport: new(operatorv2beta1.ControlPlaneDrainSupportState("invalid")),
							},
						},
					},
				},
				ExpectedErrorMessage: new("spec.translation.drainSupport: Unsupported value: \"invalid\": supported values: \"enabled\", \"disabled\""),
			},
			{
				Name: "both combinedServicesFromDifferentHTTPRoutes and drainSupport set",
				TestObject: &operatorv2beta1.ControlPlane{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv2beta1.ControlPlaneSpec{
						DataPlane: validDataPlaneTarget,
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: new("kong"),
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								CombinedServicesFromDifferentHTTPRoutes: new(operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled),
								DrainSupport:                            new(operatorv2beta1.ControlPlaneDrainSupportStateDisabled),
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)

		t.Run("fallbackConfiguration", func(t *testing.T) {
			common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
				{
					Name: "fallbackConfiguration.useLastValidConfig set to enabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
									FallbackConfiguration: &operatorv2beta1.ControlPlaneFallbackConfiguration{
										UseLastValidConfig: new(operatorv2beta1.ControlPlaneFallbackConfigurationStateEnabled),
									},
								},
							},
						},
					},
				},
				{
					Name: "fallbackConfiguration.useLastValidConfig set to disabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
									FallbackConfiguration: &operatorv2beta1.ControlPlaneFallbackConfiguration{
										UseLastValidConfig: new(operatorv2beta1.ControlPlaneFallbackConfigurationStateDisabled),
									},
								},
							},
						},
					},
				},
				{
					Name: "fallbackConfiguration.useLastValidConfig set to disallowed value",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
									FallbackConfiguration: &operatorv2beta1.ControlPlaneFallbackConfiguration{
										UseLastValidConfig: new(operatorv2beta1.ControlPlaneFallbackConfigurationState("invalid")),
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("spec.translation.fallbackConfiguration.useLastValidConfig: Unsupported value: \"invalid\": supported values: \"enabled\", \"disabled\""),
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})

		t.Run("configDump", func(t *testing.T) {
			common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
				{
					Name: "configDump.state and configDump.dumpsensitive set to enabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
									State:         operatorv2beta1.ConfigDumpStateEnabled,
									DumpSensitive: operatorv2beta1.ConfigDumpStateEnabled,
								},
							},
						},
					},
				},
				{
					Name: "configDump.state and configDump.dumpSensitive set to disabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
									State:         operatorv2beta1.ConfigDumpStateDisabled,
									DumpSensitive: operatorv2beta1.ConfigDumpStateDisabled,
								},
							},
						},
					},
				},
				{
					Name: "configDump.state set to enabled and configDump.dumpSensitive set to disabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
									State:         operatorv2beta1.ConfigDumpStateEnabled,
									DumpSensitive: operatorv2beta1.ConfigDumpStateDisabled,
								},
							},
						},
					},
				},
				{
					Name: "configDump.state set to disabled and configDump.dumpSensitive set to enabled is invalid",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
									State:         operatorv2beta1.ConfigDumpStateDisabled,
									DumpSensitive: operatorv2beta1.ConfigDumpStateEnabled,
								},
							},
						},
					},
					ExpectedErrorMessage: new("Cannot enable dumpSensitive when state is disabled"),
				},
				{
					Name: "configDump.state set to disallowed value",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
									State:         operatorv2beta1.ConfigDumpState("invalid"),
									DumpSensitive: operatorv2beta1.ConfigDumpStateEnabled,
								},
							},
						},
					},
					ExpectedErrorMessage: new(`spec.configDump.state: Unsupported value: "invalid": supported values: "enabled", "disabled"`),
				},
				{
					Name: "configDump.dumpSensitive is set to disallowed value",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
									State:         operatorv2beta1.ConfigDumpStateEnabled,
									DumpSensitive: operatorv2beta1.ConfigDumpState("invalid"),
								},
							},
						},
					},
					ExpectedErrorMessage: new(`spec.configDump.dumpSensitive: Unsupported value: "invalid": supported values: "enabled", "disabled"`),
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})

		t.Run("objectFilters", func(t *testing.T) {
			common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
				{
					Name: "objectFilters.secrets and objectFilters.configMaps are set",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ObjectFilters: &operatorv2beta1.ControlPlaneObjectFilters{
									Secrets: &operatorv2beta1.ControlPlaneFilterForObjectType{
										MatchLabels: map[string]string{"konghq.com/secret": "true"},
									},
									ConfigMaps: &operatorv2beta1.ControlPlaneFilterForObjectType{
										MatchLabels: map[string]string{"konghq.com/configmap": "true"},
									},
								},
							},
						},
					},
				},
				{
					Name: "maximum items in matchLabels is 8",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ObjectFilters: &operatorv2beta1.ControlPlaneObjectFilters{
									Secrets: &operatorv2beta1.ControlPlaneFilterForObjectType{
										MatchLabels: map[string]string{
											"konghq.com/secret": "true",
											"label1":            "value1",
											"label2":            "value2",
											"label3":            "value3",
											"label4":            "value4",
											"label5":            "value5",
											"label6":            "value6",
											"label7":            "value7",
											"label8":            "value8",
										},
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("spec.objectFilters.secrets.matchLabels: Too many: 9: must have at most 8 items"),
				},
				{
					Name: "key of objectFilters.*.matchLabels must have minimum length 1",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ObjectFilters: &operatorv2beta1.ControlPlaneObjectFilters{
									Secrets: &operatorv2beta1.ControlPlaneFilterForObjectType{
										MatchLabels: map[string]string{"konghq.com/secret": "true"},
									},
									ConfigMaps: &operatorv2beta1.ControlPlaneFilterForObjectType{
										MatchLabels: map[string]string{"": "aaa"},
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("Minimum length of key in matchLabels is 1"),
				},
				{
					Name: "value of objectFilters.*.matchLabels must have maximum length 63",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								ObjectFilters: &operatorv2beta1.ControlPlaneObjectFilters{
									Secrets: &operatorv2beta1.ControlPlaneFilterForObjectType{
										MatchLabels: map[string]string{"konghq.com/secret": "this-is-a-very-very-long-label-which-is-longer-than-63-characters"},
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("Maximum length of value in matchLabels is 63"),
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})
	})

	t.Run("konnect", func(t *testing.T) {
		t.Run("basic configuration", func(t *testing.T) {
			common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
				{
					Name: "no konnect configuration",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
							},
						},
					},
				},
				{
					Name: "konnect configuration with all options set",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									ConsumersSync: new(operatorv2beta1.ControlPlaneKonnectConsumersSyncStateEnabled),
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:                new(operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled),
										InitialPollingPeriod: new(metav1.Duration{Duration: 30 * time.Second}),
										PollingPeriod:        new(metav1.Duration{Duration: 300 * time.Second}),
										StorageState:         new(operatorv2beta1.ControlPlaneKonnectLicenseStorageStateEnabled),
									},
									NodeRefreshPeriod:  new(metav1.Duration{Duration: 60 * time.Second}),
									ConfigUploadPeriod: new(metav1.Duration{Duration: 30 * time.Second}),
								},
							},
						},
					},
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})

		t.Run("consumersSync", func(t *testing.T) {
			common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
				{
					Name: "consumersSync set to enabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									ConsumersSync: new(operatorv2beta1.ControlPlaneKonnectConsumersSyncStateEnabled),
								},
							},
						},
					},
				},
				{
					Name: "consumersSync set to disabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									ConsumersSync: new(operatorv2beta1.ControlPlaneKonnectConsumersSyncStateDisabled),
								},
							},
						},
					},
				},
				{
					Name: "consumersSync set to disallowed value",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									ConsumersSync: new(operatorv2beta1.ControlPlaneKonnectConsumersSyncState("invalid")),
								},
							},
						},
					},
					ExpectedErrorMessage: new("spec.konnect.consumersSync: Unsupported value: \"invalid\": supported values: \"enabled\", \"disabled\""),
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})

		t.Run("licensing", func(t *testing.T) {
			common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
				{
					Name: "licensing set to enabled without polling periods is allowed",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State: new(operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled),
									},
								},
							},
						},
					},
				},
				{
					Name: "licensing set to disabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:        new(operatorv2beta1.ControlPlaneKonnectLicensingStateDisabled),
										StorageState: new(operatorv2beta1.ControlPlaneKonnectLicenseStorageStateDisabled),
									},
								},
							},
						},
					},
				},
				{
					Name: "licensing with polling periods and storage",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:                new(operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled),
										InitialPollingPeriod: new(metav1.Duration{Duration: 30 * time.Second}),
										PollingPeriod:        new(metav1.Duration{Duration: 300 * time.Second}),
										StorageState:         new(operatorv2beta1.ControlPlaneKonnectLicenseStorageStateEnabled),
									},
								},
							},
						},
					},
				},
				{
					Name: "licensing with storage disabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:                new(operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled),
										InitialPollingPeriod: new(metav1.Duration{Duration: 30 * time.Second}),
										PollingPeriod:        new(metav1.Duration{Duration: 300 * time.Second}),
										StorageState:         new(operatorv2beta1.ControlPlaneKonnectLicenseStorageStateDisabled),
									},
								},
							},
						},
					},
				},
				{
					Name: "licensing storage set to disallowed value",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:        new(operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled),
										StorageState: new(operatorv2beta1.ControlPlaneKonnectLicenseStorageState("invalid")),
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("spec.konnect.licensing.storageState: Unsupported value: \"invalid\": supported values: \"enabled\", \"disabled\""),
				},
				{
					Name: "storageState set when licensing is disabled",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:        new(operatorv2beta1.ControlPlaneKonnectLicensingStateDisabled),
										StorageState: new(operatorv2beta1.ControlPlaneKonnectLicenseStorageStateEnabled),
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("storageState can only be set to enabled when licensing is enabled"),
				},
				{
					Name: "licensing set to disabled with initialPollingPeriod",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:                new(operatorv2beta1.ControlPlaneKonnectLicensingStateDisabled),
										InitialPollingPeriod: new(metav1.Duration{Duration: 30 * time.Second}),
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("initialPollingPeriod can only be set when licensing is enabled"),
				},
				{
					Name: "licensing set to disabled with pollingPeriod",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State:         new(operatorv2beta1.ControlPlaneKonnectLicensingStateDisabled),
										PollingPeriod: new(metav1.Duration{Duration: 300 * time.Second}),
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("pollingPeriod can only be set when licensing is enabled"),
				},
				{
					Name: "licensing set to disallowed value",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
										State: new(operatorv2beta1.ControlPlaneKonnectLicensingState("invalid")),
									},
								},
							},
						},
					},
					ExpectedErrorMessage: new("spec.konnect.licensing.state: Unsupported value: \"invalid\": supported values: \"enabled\", \"disabled\""),
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})

		t.Run("periods", func(t *testing.T) {
			common.TestCasesGroup[*operatorv2beta1.ControlPlane]{
				{
					Name: "nodeRefreshPeriod set",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									NodeRefreshPeriod: new(metav1.Duration{Duration: 60 * time.Second}),
								},
							},
						},
					},
				},
				{
					Name: "configUploadPeriod set",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									ConfigUploadPeriod: new(metav1.Duration{Duration: 30 * time.Second}),
								},
							},
						},
					},
				},
				{
					Name: "both periods set",
					TestObject: &operatorv2beta1.ControlPlane{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec: operatorv2beta1.ControlPlaneSpec{
							DataPlane: validDataPlaneTarget,
							ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
								IngressClass: new("kong"),
								Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
									NodeRefreshPeriod:  new(metav1.Duration{Duration: 60 * time.Second}),
									ConfigUploadPeriod: new(metav1.Duration{Duration: 30 * time.Second}),
								},
							},
						},
					},
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})
	})
}
