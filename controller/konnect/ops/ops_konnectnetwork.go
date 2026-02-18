package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnectretry "github.com/Kong/sdk-konnect-go/retry"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

// createKonnectNetwork creates the Konnect Network as specified in provided spec.
func createKonnectNetwork(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayNetwork,
) error {
	resp, err := sdk.CreateNetwork(ctx, cloudGatewayNetworkToCreateNetworkRequest(n.Spec))

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, n); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Network == nil || resp.Network.ID == "" {
		return fmt.Errorf("failed creating %s: %w", n.GetTypeName(), ErrNilResponse)
	}

	// At this point, the ControlPlane has been created in Konnect.
	id := resp.Network.ID
	n.SetKonnectID(id)
	n.Status.State = string(resp.Network.GetState())

	return nil
}

// updateKonnectNetwork updates a Konnect ControlPlane.
// NOTE: Konnect Networks are considered immutable, so this function does not
// update the Konnect Network. It only retrieves the Konnect Network's state.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
func updateKonnectNetwork(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayNetwork,
) error {
	id := n.GetKonnectStatus().GetKonnectID()
	resp, err := sdk.GetNetwork(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, GetOp, n); errWrap != nil {
		return errWrap
	}
	if resp == nil || resp.Network == nil {
		return fmt.Errorf("failed getting Network: %w", ErrNilResponse)
	}
	n.SetKonnectID(resp.Network.ID)
	// Assign the state so that it's visible in the status.
	n.Status.State = string(resp.Network.GetState())

	return nil
}

// deleteKonnectNetwork deletes a Konnect Network.
// It is assumed that the Konnect Network has a Konnect ID.
func deleteKonnectNetwork(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayNetwork,
) error {
	id := n.GetKonnectStatus().GetKonnectID()
	// Override retries as we do not want to block the reconciliation loop.
	_, err := sdk.DeleteNetwork(ctx, id, sdkkonnectops.WithRetries(sdkkonnectretry.Config{}))
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, n); errWrap != nil {
		return handleDeleteError(ctx, err, n)
	}

	return nil
}

// getKonnectNetworkMatchingSpecName returns the Konnect ID of the Konnect Network
// that matches the name of the provided Konnect Network.
func getKonnectNetworkMatchingSpecName(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayNetwork,
) (string, error) {
	reqList := sdkkonnectops.ListNetworksRequest{
		Filter: &sdkkonnectcomp.NetworksFilterParameters{
			// NOTE: can't use labels as Konnect Networks do not support labels
			// at this moment so use the name filter to get the network matching the name
			// of the reconciled KonnectNetwork.
			Name: &sdkkonnectcomp.CloudGatewaysStringFieldFilterOverride{
				StringFieldOEQFilter: &sdkkonnectcomp.StringFieldOEQFilter{
					Oeq: n.Spec.Name,
				},
				Type: sdkkonnectcomp.CloudGatewaysStringFieldFilterOverrideTypeStringFieldOEQFilter,
			},
		},
	}

	resp, err := sdk.ListNetworks(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", n.GetTypeName(), err)
	}

	if resp == nil || resp.ListNetworksResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", n.GetTypeName(), ErrNilResponse)
	}

	id, err := getMatchingEntryFromListResponseData(sliceToEntityWithIDSlice(resp.ListNetworksResponse.Data), n)
	if err != nil {
		return "", err
	}

	return id, nil
}

func cloudGatewayNetworkToCreateNetworkRequest(s konnectv1alpha1.KonnectCloudGatewayNetworkSpec) sdkkonnectcomp.CreateNetworkRequest {
	return sdkkonnectcomp.CreateNetworkRequest{
		Name:                          s.Name,
		Region:                        s.Region,
		CloudGatewayProviderAccountID: s.CloudGatewayProviderAccountID,
		AvailabilityZones:             s.AvailabilityZones,
		CidrBlock:                     s.CidrBlock,
		State:                         s.State,
	}
}

// adoptKonnectCloudGatewayNetwork adopts an existing Konnect Network.
// It fetches the network from Konnect and verifies that the spec matches the remote configuration.
// Only match mode adoption is supported for Network resources.
func adoptKonnectCloudGatewayNetwork(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayNetwork,
	adoptOptions commonv1alpha1.AdoptOptions,
) error {
	if adoptOptions.Konnect == nil || adoptOptions.Konnect.ID == "" {
		return fmt.Errorf("konnect ID must be provided for adoption")
	}
	if adoptOptions.Mode != "" && adoptOptions.Mode != commonv1alpha1.AdoptModeMatch {
		return fmt.Errorf("only match mode adoption is supported for cloud gateway network, got mode: %q", adoptOptions.Mode)
	}

	konnectID := adoptOptions.Konnect.ID
	resp, err := sdk.GetNetwork(ctx, konnectID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{KonnectID: konnectID, Err: err}
	}
	if resp == nil || resp.Network == nil {
		return fmt.Errorf("failed getting %s: %w", n.GetTypeName(), ErrNilResponse)
	}

	if diffs := compareNetworkSpec(n.Spec, resp.Network); len(diffs) > 0 {
		return KonnectEntityAdoptionNotMatchError{KonnectID: konnectID}
	}

	n.SetKonnectID(resp.Network.ID)
	n.Status.State = string(resp.Network.GetState())
	return nil
}

// compareNetworkSpec compares the Network spec with the remote Konnect Network.
// Returns a list of differences, empty if they match.
func compareNetworkSpec(spec konnectv1alpha1.KonnectCloudGatewayNetworkSpec, remote *sdkkonnectcomp.Network) []string {
	var diffs []string
	if spec.Name != remote.GetName() {
		diffs = append(diffs, fmt.Sprintf("name spec=%q konnect=%q", spec.Name, remote.GetName()))
	}
	if spec.CloudGatewayProviderAccountID != remote.GetCloudGatewayProviderAccountID() {
		diffs = append(diffs, fmt.Sprintf(
			"cloud_gateway_provider_account_id spec=%q konnect=%q",
			spec.CloudGatewayProviderAccountID,
			remote.GetCloudGatewayProviderAccountID(),
		))
	}
	if spec.Region != remote.GetRegion() {
		diffs = append(diffs, fmt.Sprintf("region spec=%q konnect=%q", spec.Region, remote.GetRegion()))
	}
	if !lo.ElementsMatch(spec.AvailabilityZones, remote.GetAvailabilityZones()) {
		diffs = append(diffs, fmt.Sprintf(
			"availability_zones spec=%v konnect=%v",
			spec.AvailabilityZones,
			remote.GetAvailabilityZones(),
		))
	}
	if spec.CidrBlock != remote.GetCidrBlock() {
		diffs = append(diffs, fmt.Sprintf("cidr_block spec=%q konnect=%q", spec.CidrBlock, remote.GetCidrBlock()))
	}
	return diffs
}
