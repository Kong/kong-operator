package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

func TestKonnectDataPlaneGroupConfiguration(t *testing.T) {
	cpRef := commonv1alpha1.ControlPlaneRef{
		Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-konnect-control-plane-cloud-gateway",
		},
	}
	networkRef := konnectv1alpha1.NetworkRef{
		Type:      "konnectID",
		KonnectID: lo.ToPtr("12345"),
	}
	t.Run("spec", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration]{
			{
				Name: "all required fields are set",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRef,
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
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRef,
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
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRef,
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
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRef,
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
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRef,
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
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						ControlPlaneRef: cpRef,
						Version:         "3.9",
						APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider:   sdkkonnectcomp.ProviderNameAws,
								Region:     "us-west-2",
								NetworkRef: networkRef,
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
}
