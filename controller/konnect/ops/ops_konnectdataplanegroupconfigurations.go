package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// createKonnectDataPlaneGroupConfiguration creates the Konnect DataPlane configuration as specified in provided spec.
func createKonnectDataPlaneGroupConfiguration(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
) error {
	cpID := n.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: n, Op: CreateOp}
	}

	req := cloudGatewayDataPlaneGroupConfigurationToAPIRequest(n.Spec, cpID)
	resp, err := sdk.CreateConfiguration(ctx, req)

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, n); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.ConfigurationManifest == nil || resp.ConfigurationManifest.ID == "" {
		return fmt.Errorf("failed creating %s: %w", n.GetTypeName(), ErrNilResponse)
	}

	// At this point, the DataPlaneGroupConfiguration has been created in Konnect.
	id := resp.ConfigurationManifest.ID
	n.SetKonnectID(id)
	n.Status.DataPlaneGroups = dataPlaneGroupsResponseToStatus(resp.ConfigurationManifest.GetDataplaneGroups())

	return nil
}

// updateKonnectDataPlaneGroupConfiguration updates a Konnect DataPlaneGroupConfiguration.
func updateKonnectDataPlaneGroupConfiguration(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
) error {
	cpID := n.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: n, Op: UpdateOp}
	}

	req := cloudGatewayDataPlaneGroupConfigurationToAPIRequest(n.Spec, cpID)
	resp, err := sdk.CreateConfiguration(ctx, req)
	if err != nil {
		var transientError bool

		// NOTE: Cloud Gateways Data Plane group configuration API
		// https://docs.konghq.com/konnect/api/cloud-gateways/latest/#/Data-Plane%20Group%20Configurations/create-configuration
		// is not idempotent and will return a 409 Conflict error if the configuration
		// is the same as the previous one. In this case, we ignore the error and
		// perform a lookup to get the current configuration.
		if errorIsDataPlaneGroupConflictProposedConfigIsTheSame(err) {
			transientError = true
		}

		// NOTE: Cloud Gateways Data Plane group configuration API
		// https://docs.konghq.com/konnect/api/cloud-gateways/latest/#/Data-Plane%20Group%20Configurations/create-configuration
		// is not idempotent and will return a 409 Conflict error if the previous
		// configuration is not finished provisioning. In this case, we ignore the
		// error and perform a lookup to get the current configuration.
		if errorIsDataPlaneGroupBadRequestPreviousConfigNotFinishedProvisioning(err) {
			transientError = true
		}

		if transientError {
			id := n.GetKonnectID()
			resp, err := sdk.GetConfiguration(ctx, id)
			if errWrap := wrapErrIfKonnectOpFailed(err, GetOp, n); errWrap != nil {
				return errWrap
			}
			if resp == nil || resp.ConfigurationManifest == nil {
				return fmt.Errorf("failed getting %s: %w", n.GetTypeName(), ErrNilResponse)
			}
			n.SetKonnectID(resp.ConfigurationManifest.ID)
			n.Status.DataPlaneGroups = dataPlaneGroupsResponseToStatus(resp.ConfigurationManifest.GetDataplaneGroups())
			return nil
		}

		// If there was an error which wasn't a conflict, complaining about submitting
		// the same configuration, wrap it and return it.
		if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, n); errWrap != nil {
			return errWrap
		}
	}

	if resp == nil || resp.ConfigurationManifest == nil || resp.ConfigurationManifest.ID == "" {
		return fmt.Errorf("failed updating %s: %w", n.GetTypeName(), ErrNilResponse)
	}

	// At this point, the DataPlaneGroupConfiguration has been created in Konnect.
	id := resp.ConfigurationManifest.ID
	n.SetKonnectID(id)
	n.Status.DataPlaneGroups = dataPlaneGroupsResponseToStatus(resp.ConfigurationManifest.GetDataplaneGroups())

	// sdk.ListConfigurations(ctx, sdkkonnectops.ListConfigurationsRequest{
	// 	Filter: &sdkkonnectcomp.ConfigurationsFilterParameters{
	// 		ControlPlaneID:  *sdkkonnectcomp.IDFieldFilter,
	// 		ControlPlaneGeo: *sdkkonnectcomp.ControlPlaneGeoFieldFilter,
	// 	},
	// })

	return nil
}

// deleteKonnectDataPlaneGroupConfiguration deletes a Konnect DataPlaneGroupConfiguration.
// It is assumed that the Konnect DataPlaneGroupConfiguration has a Konnect ID.
func deleteKonnectDataPlaneGroupConfiguration(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
) error {
	cpID := n.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: n, Op: DeleteOp}
	}
	// NOTE: we delete the data plane group configuration by "creating" (using PUT)
	// a new configuration with the same ID and the same version, but with an empty
	// list of data plane groups.
	req := cloudGatewayDataPlaneGroupConfigurationInit(n.Spec, cpID)
	resp, err := sdk.CreateConfiguration(ctx, req)

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, n); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.ConfigurationManifest == nil || resp.ConfigurationManifest.ID == "" {
		return fmt.Errorf("failed deleting %s: %w", n.GetTypeName(), ErrNilResponse)
	}

	return nil
}

func cloudGatewayDataPlaneGroupConfigurationInit(
	spec konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec,
	cpID string,
) sdkkonnectcomp.CreateConfigurationRequest {
	return sdkkonnectcomp.CreateConfigurationRequest{
		ControlPlaneID: cpID,
		Version:        spec.Version,
		APIAccess:      spec.APIAccess,
		// TODO deduct CP geo
		ControlPlaneGeo: sdkkonnectcomp.ControlPlaneGeoEu,
		DataplaneGroups: []sdkkonnectcomp.CreateConfigurationDataPlaneGroup{},
	}
}

func cloudGatewayDataPlaneGroupConfigurationToAPIRequest(
	spec konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec,
	cpID string,
) sdkkonnectcomp.CreateConfigurationRequest {
	cfgReq := cloudGatewayDataPlaneGroupConfigurationInit(spec, cpID)
	cfgReq.DataplaneGroups = func() []sdkkonnectcomp.CreateConfigurationDataPlaneGroup {
		ret := make([]sdkkonnectcomp.CreateConfigurationDataPlaneGroup, 0, len(spec.DataplaneGroups))
		for _, g := range spec.DataplaneGroups {
			ret = append(ret, konnectConfigurationDataPlaneGroupToAPIRequest(g))
		}
		return ret
	}()

	return cfgReq
}

func konnectConfigurationDataPlaneGroupToAPIRequest(
	spec konnectv1alpha1.KonnectConfigurationDataPlaneGroup,
) sdkkonnectcomp.CreateConfigurationDataPlaneGroup {
	return sdkkonnectcomp.CreateConfigurationDataPlaneGroup{
		Provider: spec.Provider,
		Region:   spec.Region,
		Environment: func() []sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField {
			ret := make([]sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField, 0, len(spec.Environment))
			for _, e := range spec.Environment {
				ret = append(ret, sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField{
					Name:  e.Name,
					Value: e.Value,
				})
			}
			return ret
		}(),
		CloudGatewayNetworkID: func() string {
			switch spec.NetworkRef.Type {
			case konnectv1alpha1.NetworkRefKonnectID:
				return *spec.NetworkRef.KonnectID
			default:
				panic(fmt.Sprintf("unknown network ref type: %s", spec.NetworkRef.Type))
			}
		}(),
		Autoscale: func() sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale {
			switch spec.Autoscale.Type {
			case konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot:
				return sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale{
					Type: sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleTypeConfigurationDataPlaneGroupAutoscaleAutopilot,
					ConfigurationDataPlaneGroupAutoscaleAutopilot: &sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilot{
						Kind:    sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilotKindAutopilot,
						BaseRps: spec.Autoscale.Autopilot.BaseRps,
						MaxRps:  spec.Autoscale.Autopilot.MaxRps,
					},
				}
			case konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic:
				return sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale{
					Type: sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleTypeConfigurationDataPlaneGroupAutoscaleStatic,
					ConfigurationDataPlaneGroupAutoscaleStatic: &sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleStatic{
						Kind:               sdkkonnectcomp.KindStatic,
						InstanceType:       spec.Autoscale.Static.InstanceType,
						RequestedInstances: spec.Autoscale.Static.RequestedInstances,
					},
				}
			default:
				panic(fmt.Sprintf("unknown autoscale type: %s", spec.Autoscale.Type))
			}
		}(),
	}
}

func dataPlaneGroupsResponseToStatus(
	r []sdkkonnectcomp.ConfigurationDataPlaneGroup,
) []konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationStatusGroup {
	ret := make([]konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationStatusGroup, 0, len(r))
	for _, g := range r {
		ret = append(
			ret,
			konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationStatusGroup{
				ID:                    g.ID,
				State:                 string(g.State),
				CloudGatewayNetworkID: g.CloudGatewayNetworkID,
				PrivateIPAddresses:    g.PrivateIPAddresses,
				EgressIPAddresses:     g.EgressIPAddresses,
				Provider:              g.Provider,
				Region:                g.Region,
			},
		)
	}
	return ret
}
