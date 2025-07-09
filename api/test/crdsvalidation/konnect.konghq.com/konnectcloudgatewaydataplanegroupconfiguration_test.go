package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKonnectDataPlaneGroupConfiguration(t *testing.T) {
	cpRef := commonv1alpha1.ControlPlaneRef{
		Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-konnect-control-plane-cloud-gateway",
		},
	}
	networkRefKonnectID := commonv1alpha1.ObjectRef{
		Type:      "konnectID",
		KonnectID: lo.ToPtr("12345"),
	}
	autoscaleConfiguration := konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
		Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic,
		Static: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleStatic{
			InstanceType:       sdkkonnectcomp.InstanceTypeNameSmall,
			RequestedInstances: 3,
		},
	}

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration]{
			{
				Name: "all required fields are set",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRefKonnectID,
								Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
									Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic,
									Static: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleStatic{
										InstanceType:       sdkkonnectcomp.InstanceTypeNameSmall,
										RequestedInstances: 3,
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "autopilot: providing autopilot configuration when type is static should fail",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRefKonnectID,
								Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
									Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic,
									Autopilot: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleAutopilot{
										BaseRps: 123,
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("static is required when type is static"),
			},
			{
				Name: "autopilot: providing static configuration when type is autopilot should fail",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRefKonnectID,
								Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
									Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot,
									Static: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleStatic{
										InstanceType:       sdkkonnectcomp.InstanceTypeNameSmall,
										RequestedInstances: 3,
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("autopilot is required when type is autopilot"),
			},
			{
				Name: "can't provide both autopilot and static configuration",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRefKonnectID,
								Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
									Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot,
									Static: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleStatic{
										InstanceType:       sdkkonnectcomp.InstanceTypeNameSmall,
										RequestedInstances: 3,
									},
									Autopilot: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleAutopilot{
										BaseRps: 123,
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("can't provide both autopilot and static configuration"),
			},
			{
				Name: "envs have to start with KONG_",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRefKonnectID,
								Environment: []konnectv1alpha1.ConfigurationDataPlaneGroupEnvironmentField{
									{
										Name:  "KONG_LOG_LEVEL",
										Value: "debug",
									},
								},
								Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
									Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic,
									Static: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleStatic{
										InstanceType:       sdkkonnectcomp.InstanceTypeNameSmall,
										RequestedInstances: 3,
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "envs that do not start with KONG_ cause errors",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRefKonnectID,
								Environment: []konnectv1alpha1.ConfigurationDataPlaneGroupEnvironmentField{
									{
										Name:  "RANDOM_ENV",
										Value: "debug",
									},
								},
								Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
									Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic,
									Static: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleStatic{
										InstanceType:       sdkkonnectcomp.InstanceTypeNameSmall,
										RequestedInstances: 3,
									},
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Invalid value: \"RANDOM_ENV\": spec.dataplane_groups[0].environment[0].name in body should match '^KONG_."),
			},
		}.Run(t)
	})

	t.Run("networkRef", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration]{
			{
				Name: "networkRef konnectID is supported",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider: sdkkonnectcomp.ProviderNameAws,
								Region:   "us-west-2",
								NetworkRef: commonv1alpha1.ObjectRef{
									Type:      "konnectID",
									KonnectID: lo.ToPtr("12345"),
								},
								Autoscale: autoscaleConfiguration,
							},
						},
					},
				},
			},
			{
				Name: "networkRef konnectID is required when type is konnectID",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider: sdkkonnectcomp.ProviderNameAws,
								Region:   "us-west-2",
								NetworkRef: commonv1alpha1.ObjectRef{
									Type: "konnectID",
									NamespacedRef: &commonv1alpha1.NamespacedRef{
										Name: "network-1",
									},
								},
								Autoscale: autoscaleConfiguration,
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "networkRef namespacedRef is supported",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider: sdkkonnectcomp.ProviderNameAws,
								Region:   "us-west-2",
								NetworkRef: commonv1alpha1.ObjectRef{
									Type: "namespacedRef",
									NamespacedRef: &commonv1alpha1.NamespacedRef{
										Name: "network-1",
									},
								},
								Autoscale: autoscaleConfiguration,
							},
						},
					},
				},
			},
			{
				Name: "networkRef namespacedRef is required when type is namespacedRef",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider: sdkkonnectcomp.ProviderNameAws,
								Region:   "us-west-2",
								NetworkRef: commonv1alpha1.ObjectRef{
									Type:      "namespacedRef",
									KonnectID: lo.ToPtr("12345"),
								},
								Autoscale: autoscaleConfiguration,
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is namespacedRef, namespacedRef must be set"),
			},
			{
				Name: "networkRef namespacedRef cannot specify namespace",
				SkipReason: "cross namespace references are not allowed but using the CEL reserved fields like 'namespace' " +
					"is only allowed in Kubernetes 1.32+ (https://github.com/kubernetes/kubernetes/pull/126977). " +
					"Re-enable this test and reintroduce the rule that enforces this when 1.32 becomes the oldest supported version.",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider: sdkkonnectcomp.ProviderNameAws,
								Region:   "us-west-2",
								NetworkRef: commonv1alpha1.ObjectRef{
									Type: "namespacedRef",
									NamespacedRef: &commonv1alpha1.NamespacedRef{
										Name:      "network-1",
										Namespace: lo.ToPtr("ns-1"),
									},
								},
								Autoscale: autoscaleConfiguration,
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("cross namespace references are not supported for networkRef of type namespacedRef"),
			},
		}.Run(t)
	})
}
