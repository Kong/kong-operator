package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/konnect/server"
)

// createKonnectDataPlaneGroupConfiguration creates the Konnect DataPlane configuration as specified in provided spec.
func createKonnectDataPlaneGroupConfiguration(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	cl client.Client,
	n *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
	serverRegion server.Region,
) error {
	cpID := n.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: n, Op: CreateOp}
	}

	req, err := cloudGatewayDataPlaneGroupConfigurationToAPIRequest(ctx, cl, n.Spec, n.Namespace, cpID, serverRegion)
	if err != nil {
		return fmt.Errorf("failed to convert configuration spec: %w", err)
	}

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
	cl client.Client,
	n *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
	server server.Server,
) error {
	cpID := n.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: n, Op: UpdateOp}
	}

	req, err := cloudGatewayDataPlaneGroupConfigurationToAPIRequest(ctx, cl, n.Spec, n.Namespace, cpID, server.Region())
	if err != nil {
		return fmt.Errorf("failed to convert configuration spec: %w", err)
	}

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

	return nil
}

// deleteKonnectDataPlaneGroupConfiguration deletes a Konnect DataPlaneGroupConfiguration.
// It is assumed that the Konnect DataPlaneGroupConfiguration has a Konnect ID.
func deleteKonnectDataPlaneGroupConfiguration(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
	serverRegion server.Region,
) error {
	cpID := n.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: n, Op: DeleteOp}
	}
	// NOTE: we delete the data plane group configuration by "creating" (using PUT)
	// a new configuration with the same ID and the same version, but with an empty
	// list of data plane groups.
	req := cloudGatewayDataPlaneGroupConfigurationInit(n.Spec, cpID, serverRegion)
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
	serverRegion server.Region,
) sdkkonnectcomp.CreateConfigurationRequest {
	// We intentionally map the server region to a Konnect SDK ControlPlaneGeo value without validation to make this
	// forward-compatible with future server regions that are not yet defined in the Konnect SDK.
	cpGeo := sdkkonnectcomp.ControlPlaneGeo(serverRegion.String())

	return sdkkonnectcomp.CreateConfigurationRequest{
		ControlPlaneID:  cpID,
		Version:         spec.Version,
		APIAccess:       spec.APIAccess,
		ControlPlaneGeo: cpGeo,
		DataplaneGroups: []sdkkonnectcomp.CreateConfigurationDataPlaneGroup{},
	}
}

func cloudGatewayDataPlaneGroupConfigurationToAPIRequest(
	ctx context.Context,
	cl client.Client,
	spec konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec,
	namespace string,
	cpID string,
	cpRegion server.Region,
) (sdkkonnectcomp.CreateConfigurationRequest, error) {
	cfgReq := cloudGatewayDataPlaneGroupConfigurationInit(spec, cpID, cpRegion)

	dataplaneGroups := make([]sdkkonnectcomp.CreateConfigurationDataPlaneGroup, 0, len(spec.DataplaneGroups))
	for _, g := range spec.DataplaneGroups {
		dpg, err := konnectConfigurationDataPlaneGroupToAPIRequest(ctx, cl, g, namespace)
		if err != nil {
			// This should never happen, since we validate the spec at the CRD level.
			return sdkkonnectcomp.CreateConfigurationRequest{}, fmt.Errorf("failed to convert data plane group: %w", err)
		}
		dataplaneGroups = append(dataplaneGroups, dpg)
	}
	cfgReq.DataplaneGroups = dataplaneGroups

	return cfgReq, nil
}

func konnectConfigurationDataPlaneGroupToAPIRequest(
	ctx context.Context,
	cl client.Client,
	spec konnectv1alpha1.KonnectConfigurationDataPlaneGroup,
	namespace string,
) (sdkkonnectcomp.CreateConfigurationDataPlaneGroup, error) {
	var networkID string
	switch spec.NetworkRef.Type {
	case commonv1alpha1.ObjectRefTypeKonnectID:
		networkID = *spec.NetworkRef.KonnectID
	case commonv1alpha1.ObjectRefTypeNamespacedRef:
		var network konnectv1alpha1.KonnectCloudGatewayNetwork
		nn := types.NamespacedName{
			Name:      spec.NetworkRef.NamespacedRef.Name,
			Namespace: namespace,
		}
		if err := cl.Get(ctx, nn, &network); err != nil {
			return sdkkonnectcomp.CreateConfigurationDataPlaneGroup{}, fmt.Errorf("failed to get network %s: %w", nn, err)
		}
		// Just check if the network has an ID.
		// Other aspects of network readiness are checks in handleKonnectNetworkRef.
		if network.Status.ID == "" {
			return sdkkonnectcomp.CreateConfigurationDataPlaneGroup{}, fmt.Errorf("network %s has no ID", nn)
		}
		networkID = network.Status.ID
	default:
		return sdkkonnectcomp.CreateConfigurationDataPlaneGroup{}, fmt.Errorf("unknown network ref type: %s", spec.NetworkRef.Type)
	}

	autoscaleConf, err := configurationDataPlaneGroupAutoscaleTypeToSDKAutoscale(spec.Autoscale)
	if err != nil {
		return sdkkonnectcomp.CreateConfigurationDataPlaneGroup{}, fmt.Errorf("failed to convert autoscale type: %w", err)
	}

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
		CloudGatewayNetworkID: networkID,
		Autoscale:             autoscaleConf,
	}, nil
}

func configurationDataPlaneGroupAutoscaleTypeToSDKAutoscale(
	autoscale konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale,
) (sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale, error) {
	switch autoscale.Type {
	case konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot:
		return sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale{
			Type: sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleTypeConfigurationDataPlaneGroupAutoscaleAutopilot,
			ConfigurationDataPlaneGroupAutoscaleAutopilot: &sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilot{
				Kind:    sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilotKindAutopilot,
				BaseRps: autoscale.Autopilot.BaseRps,
				MaxRps:  autoscale.Autopilot.MaxRps,
			},
		}, nil
	// ConfigurationDataPlaneGroupAutoscaleTypeStatic is deprecated.
	case konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic:
		return sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale{
			Type: sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleTypeConfigurationDataPlaneGroupAutoscaleStatic,
			// ConfigurationDataPlaneGroupAutoscaleStatic is deprecated.
			ConfigurationDataPlaneGroupAutoscaleStatic: &sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleStatic{ //nolint:staticcheck
				Kind:               sdkkonnectcomp.KindStatic,
				InstanceType:       autoscale.Static.InstanceType,
				RequestedInstances: autoscale.Static.RequestedInstances,
			},
		}, nil
	default:
		return sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale{}, fmt.Errorf("unknown autoscale type: %s", autoscale.Type)
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
