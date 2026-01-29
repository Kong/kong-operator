package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/retry"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

// createKonnectTransitGateway creates a transit gateway on the Konnect side.
func createKonnectTransitGateway(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	tg *konnectv1alpha1.KonnectCloudGatewayTransitGateway,
) error {
	networkID := tg.GetNetworkID()
	if networkID == "" {
		return CantPerformOperationWithoutNetworkIDError{
			Entity: tg,
			Op:     CreateOp,
		}
	}

	// We need to set the empty retry config to prevent using the default retry which causes failures and blocks the reconciliation:
	// https://github.com/kong/kong-operator/issues/1521
	resp, err := sdk.CreateTransitGateway(
		ctx, networkID, transitGatewaySpecToTransitGatewayInput(tg.Spec.KonnectTransitGatewayAPISpec),
		sdkkonnectops.WithRetries(retry.Config{}),
	)
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, tg); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.TransitGatewayResponse == nil {
		return fmt.Errorf("failed creating %s: %w", tg.GetTypeName(), ErrNilResponse)
	}

	tg.SetKonnectID(extractKonnectIDFromTransitGatewayResponse(resp.TransitGatewayResponse))
	tg.Status.State = extractStateFromTransitGatewayResponse(resp.TransitGatewayResponse)
	return nil
}

// updateKonnectTransitGateway is called when an "Update" operation is called in reconciling a Konnect transit gateway.
// Since Konnect does not provide API to update an existing transit gateway, here we can only update the status of the
// KonnectCloudGatewayTransitGateway resource based on the state of the transit gateway on the Konnect side.
func updateKonnectTransitGateway(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	tg *konnectv1alpha1.KonnectCloudGatewayTransitGateway,
) error {
	networkID := tg.GetNetworkID()
	if networkID == "" {
		return CantPerformOperationWithoutNetworkIDError{
			Entity: tg,
			Op:     UpdateOp,
		}
	}

	resp, err := sdk.GetTransitGateway(ctx, networkID, tg.GetKonnectID())
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, tg); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.TransitGatewayResponse == nil {
		return fmt.Errorf("failed updating %s: %w", tg.GetTypeName(), ErrNilResponse)
	}

	tg.Status.State = extractStateFromTransitGatewayResponse(resp.TransitGatewayResponse)
	return nil
}

// deleteKonnectTransitGateway deletes a Konnect transit gateway.
func deleteKonnectTransitGateway(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	tg *konnectv1alpha1.KonnectCloudGatewayTransitGateway,
) error {
	networkID := tg.GetNetworkID()
	if networkID == "" {
		return CantPerformOperationWithoutNetworkIDError{
			Entity: tg,
			Op:     DeleteOp,
		}
	}

	// We need to set the empty retry config to prevent using the default retry which causes failures and blocks the reconciliation:
	// https://github.com/kong/kong-operator/issues/1521
	resp, err := sdk.DeleteTransitGateway(ctx, networkID, tg.GetKonnectID(), sdkkonnectops.WithRetries(retry.Config{}))

	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, tg); errWrap != nil {
		return errWrap
	}

	if resp == nil {
		return fmt.Errorf("failed deleting %s: %w", tg.GetTypeName(), ErrNilResponse)
	}

	return nil
}

func getKonnectTransitGatewayMatchingSpecName(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	tg *konnectv1alpha1.KonnectCloudGatewayTransitGateway,
) (string, error) {
	networkID := tg.GetNetworkID()
	if networkID == "" {
		return "", CantPerformOperationWithoutNetworkIDError{
			Entity: tg,
			Op:     CreateOp,
		}
	}

	resp, err := sdk.ListTransitGateways(ctx, sdkkonnectops.ListTransitGatewaysRequest{
		NetworkID: networkID,
		Filter: &sdkkonnectcomp.TransitGatewaysFilterParameters{
			Name: &sdkkonnectcomp.CloudGatewaysStringFieldFilterOverride{
				CloudGatewaysStringFieldEqualsFilterOverride: &sdkkonnectcomp.CloudGatewaysStringFieldEqualsFilterOverride{
					Str:  lo.ToPtr(extractNameFromKonnectCloudGatewayTransitGatewaySpec(tg.Spec.KonnectTransitGatewayAPISpec)),
					Type: sdkkonnectcomp.CloudGatewaysStringFieldEqualsFilterOverrideTypeStr,
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", tg.GetTypeName(), err)
	}

	if resp == nil || resp.ListTransitGatewaysResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", tg.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(listTransitGatewayResponseDataToEntityWithIDSlice(resp.ListTransitGatewaysResponse.Data), tg)
}

var transitGatewayTypeToSDKTransitGatewayType = map[konnectv1alpha1.TransitGatewayType]sdkkonnectcomp.CreateTransitGatewayRequestType{
	konnectv1alpha1.TransitGatewayTypeAWSTransitGateway:   sdkkonnectcomp.CreateTransitGatewayRequestTypeAWSTransitGateway,
	konnectv1alpha1.TransitGatewayTypeAzureTransitGateway: sdkkonnectcomp.CreateTransitGatewayRequestTypeAzureTransitGateway,
}

func transitGatewaySpecToTransitGatewayInput(
	spec konnectv1alpha1.KonnectTransitGatewayAPISpec,
) sdkkonnectcomp.CreateTransitGatewayRequest {
	typ := transitGatewayTypeToSDKTransitGatewayType[spec.Type]

	req := sdkkonnectcomp.CreateTransitGatewayRequest{
		Type: typ,
	}

	switch spec.Type {
	case konnectv1alpha1.TransitGatewayTypeAWSTransitGateway:
		req.AWSTransitGateway = &sdkkonnectcomp.AWSTransitGateway{
			Name: spec.AWSTransitGateway.Name,
			DNSConfig: lo.Map(spec.AWSTransitGateway.DNSConfig, func(dnsConf konnectv1alpha1.TransitGatewayDNSConfig, _ int) sdkkonnectcomp.TransitGatewayDNSConfig {
				return sdkkonnectcomp.TransitGatewayDNSConfig{
					RemoteDNSServerIPAddresses: dnsConf.RemoteDNSServerIPAddresses,
					DomainProxyList:            dnsConf.DomainProxyList,
				}
			}),
			CidrBlocks: spec.AWSTransitGateway.CIDRBlocks,
			TransitGatewayAttachmentConfig: sdkkonnectcomp.AwsTransitGatewayAttachmentConfig{
				Kind:             sdkkonnectcomp.AWSTransitGatewayAttachmentTypeAwsTransitGatewayAttachment,
				TransitGatewayID: spec.AWSTransitGateway.AttachmentConfig.TransitGatewayID,
				RAMShareArn:      spec.AWSTransitGateway.AttachmentConfig.RAMShareArn,
			},
		}
	case konnectv1alpha1.TransitGatewayTypeAzureTransitGateway:
		req.AzureTransitGateway = &sdkkonnectcomp.AzureTransitGateway{
			Name: spec.AzureTransitGateway.Name,
			DNSConfig: lo.Map(spec.AzureTransitGateway.DNSConfig, func(dnsConf konnectv1alpha1.TransitGatewayDNSConfig, _ int) sdkkonnectcomp.TransitGatewayDNSConfig {
				return sdkkonnectcomp.TransitGatewayDNSConfig{
					RemoteDNSServerIPAddresses: dnsConf.RemoteDNSServerIPAddresses,
					DomainProxyList:            dnsConf.DomainProxyList,
				}
			}),
			TransitGatewayAttachmentConfig: sdkkonnectcomp.AzureVNETPeeringAttachmentConfig{
				Kind:              sdkkonnectcomp.AzureVNETPeeringAttachmentTypeAzureVnetPeeringAttachment,
				TenantID:          spec.AzureTransitGateway.AttachmentConfig.TenantID,
				SubscriptionID:    spec.AzureTransitGateway.AttachmentConfig.SubscriptionID,
				ResourceGroupName: spec.AzureTransitGateway.AttachmentConfig.ResourceGroupName,
				VnetName:          spec.AzureTransitGateway.AttachmentConfig.VnetName,
			},
		}
	}

	return req
}

func extractKonnectIDFromTransitGatewayResponse(resp *sdkkonnectcomp.TransitGatewayResponse) string {
	switch resp.Type {
	case sdkkonnectcomp.TransitGatewayResponseTypeAwsResourceEndpointGatewayResponse:
		return resp.AwsResourceEndpointGatewayResponse.ID
	case sdkkonnectcomp.TransitGatewayResponseTypeGCPVPCPeeringGatewayResponse:
		return resp.GCPVPCPeeringGatewayResponse.ID
	case sdkkonnectcomp.TransitGatewayResponseTypeAwsTransitGatewayResponse:
		return resp.AwsTransitGatewayResponse.ID
	case sdkkonnectcomp.TransitGatewayResponseTypeAzureTransitGatewayResponse:
		return resp.AzureTransitGatewayResponse.ID
	case sdkkonnectcomp.TransitGatewayResponseTypeAwsVpcPeeringGatewayResponse:
		// AWS VPC peering gateway is not supported yet.
		// It is not valid in the KonnectCloudGatewayTransitGateway's spec.type.
		return ""
	case sdkkonnectcomp.TransitGatewayResponseTypeAzureVhubPeeringGatewayResponse:
		// Peering not supported yet.
		return ""
	}
	return ""
}

func extractStateFromTransitGatewayResponse(resp *sdkkonnectcomp.TransitGatewayResponse) sdkkonnectcomp.TransitGatewayState {
	switch resp.Type {
	case sdkkonnectcomp.TransitGatewayResponseTypeGCPVPCPeeringGatewayResponse:
		return resp.GCPVPCPeeringGatewayResponse.State
	case sdkkonnectcomp.TransitGatewayResponseTypeAwsResourceEndpointGatewayResponse:
		return resp.AwsResourceEndpointGatewayResponse.State
	case sdkkonnectcomp.TransitGatewayResponseTypeAwsTransitGatewayResponse:
		return resp.AwsTransitGatewayResponse.State
	case sdkkonnectcomp.TransitGatewayResponseTypeAzureTransitGatewayResponse:
		return resp.AzureTransitGatewayResponse.State
	case sdkkonnectcomp.TransitGatewayResponseTypeAwsVpcPeeringGatewayResponse:
		// AWS VPC peering gateway is not supported yet.
		// It is not valid in the KonnectCloudGatewayTransitGateway's spec.type.
		return sdkkonnectcomp.TransitGatewayState("")
	case sdkkonnectcomp.TransitGatewayResponseTypeAzureVhubPeeringGatewayResponse:
		// Peering not supported yet.
		return sdkkonnectcomp.TransitGatewayState("")
	}
	return sdkkonnectcomp.TransitGatewayState("")
}

func extractNameFromKonnectCloudGatewayTransitGatewaySpec(spec konnectv1alpha1.KonnectTransitGatewayAPISpec) string {
	switch spec.Type {
	case konnectv1alpha1.TransitGatewayTypeAWSTransitGateway:
		return spec.AWSTransitGateway.Name
	case konnectv1alpha1.TransitGatewayTypeAzureTransitGateway:
		return spec.AzureTransitGateway.Name
	}
	return ""
}

func listTransitGatewayResponseDataToEntityWithIDSlice(resps []sdkkonnectcomp.TransitGatewayResponse) []extractedEntityID {
	return lo.Map(resps, func(resp sdkkonnectcomp.TransitGatewayResponse, _ int) extractedEntityID {
		return extractedEntityID(extractKonnectIDFromTransitGatewayResponse(&resp))
	})
}

func adoptKonnectTransitGateway(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	tg *konnectv1alpha1.KonnectCloudGatewayTransitGateway,
	adoptOptions commonv1alpha1.AdoptOptions,
) error {
	if adoptOptions.Konnect == nil || adoptOptions.Konnect.ID == "" {
		return fmt.Errorf("konnect ID must be provided for adoption")
	}
	if adoptOptions.Mode != "" && adoptOptions.Mode != commonv1alpha1.AdoptModeMatch {
		return fmt.Errorf("only match mode adoption is supported for cloud gateway transit gateway, got mode: %q", adoptOptions.Mode)
	}

	konnectID := adoptOptions.Konnect.ID
	networkID := tg.GetNetworkID()
	if networkID == "" {
		return CantPerformOperationWithoutNetworkIDError{Entity: tg, Op: GetOp}
	}

	resp, err := sdk.GetTransitGateway(ctx, networkID, konnectID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{KonnectID: konnectID, Err: err}
	}
	if resp == nil || resp.TransitGatewayResponse == nil {
		return fmt.Errorf("failed getting %s: %w", tg.GetTypeName(), ErrNilResponse)
	}

	if diff := compareTransitGatewaySpec(tg.Spec.KonnectTransitGatewayAPISpec, resp.TransitGatewayResponse); diff != "" {
		return KonnectEntityAdoptionNotMatchError{KonnectID: konnectID}
	}

	tg.SetKonnectID(extractKonnectIDFromTransitGatewayResponse(resp.TransitGatewayResponse))
	tg.Status.State = extractStateFromTransitGatewayResponse(resp.TransitGatewayResponse)
	return nil
}

func compareTransitGatewaySpec(
	spec konnectv1alpha1.KonnectTransitGatewayAPISpec,
	remote *sdkkonnectcomp.TransitGatewayResponse,
) string {
	switch spec.Type {
	case konnectv1alpha1.TransitGatewayTypeAWSTransitGateway:
		aws := remote.AwsTransitGatewayResponse
		if aws == nil {
			return "transit gateway type mismatch"
		}
		if spec.AWSTransitGateway == nil {
			return "spec.awsTransitGateway must be provided"
		}
		if spec.AWSTransitGateway.Name != aws.GetName() {
			return fmt.Sprintf("name mismatch spec=%q konnect=%q", spec.AWSTransitGateway.Name, aws.GetName())
		}
		if !lo.ElementsMatch(spec.AWSTransitGateway.CIDRBlocks, aws.GetCidrBlocks()) {
			return fmt.Sprintf(
				"cidr_blocks mismatch spec=%v konnect=%v",
				spec.AWSTransitGateway.CIDRBlocks,
				aws.GetCidrBlocks(),
			)
		}
		if diff := compareTransitGatewayDNSConfig(spec.AWSTransitGateway.DNSConfig, aws.GetDNSConfig()); diff != "" {
			return diff
		}
		if diff := compareAwsTransitGatewayAttachment(spec.AWSTransitGateway.AttachmentConfig, aws.GetTransitGatewayAttachmentConfig()); diff != "" {
			return diff
		}
	case konnectv1alpha1.TransitGatewayTypeAzureTransitGateway:
		azure := remote.AzureTransitGatewayResponse
		if azure == nil {
			return "transit gateway type mismatch"
		}
		if spec.AzureTransitGateway == nil {
			return "spec.azureTransitGateway must be provided"
		}
		if spec.AzureTransitGateway.Name != azure.GetName() {
			return fmt.Sprintf("name mismatch spec=%q konnect=%q", spec.AzureTransitGateway.Name, azure.GetName())
		}
		if diff := compareTransitGatewayDNSConfig(spec.AzureTransitGateway.DNSConfig, azure.GetDNSConfig()); diff != "" {
			return diff
		}
		if diff := compareAzureTransitGatewayAttachment(spec.AzureTransitGateway.AttachmentConfig, azure.GetTransitGatewayAttachmentConfig()); diff != "" {
			return diff
		}
	default:
		return fmt.Sprintf("unsupported transit gateway type %q", spec.Type)
	}

	return ""
}

func compareTransitGatewayDNSConfig(
	spec []konnectv1alpha1.TransitGatewayDNSConfig,
	remote []sdkkonnectcomp.TransitGatewayDNSConfig,
) string {
	specNorm := normalizeDNSConfigs(spec)
	remoteNorm := normalizeKonnectDNSConfigs(remote)

	// Compare using JSON since the structs are already normalized and sorted
	specJSON, err := marshalNormalized(specNorm)
	if err != nil {
		return err.Error()
	}
	remoteJSON, err := marshalNormalized(remoteNorm)
	if err != nil {
		return err.Error()
	}

	if specJSON != remoteJSON {
		return fmt.Sprintf("dns_config mismatch spec=%s konnect=%s", specJSON, remoteJSON)
	}

	return ""
}

func compareAwsTransitGatewayAttachment(
	spec konnectv1alpha1.AwsTransitGatewayAttachmentConfig,
	remote sdkkonnectcomp.AwsTransitGatewayAttachmentConfig,
) string {
	if spec.TransitGatewayID != remote.GetTransitGatewayID() {
		return fmt.Sprintf(
			"attachment.transit_gateway_id mismatch spec=%q konnect=%q",
			spec.TransitGatewayID,
			remote.GetTransitGatewayID(),
		)
	}
	if spec.RAMShareArn != remote.GetRAMShareArn() {
		return fmt.Sprintf(
			"attachment.ram_share_arn mismatch spec=%q konnect=%q",
			spec.RAMShareArn,
			remote.GetRAMShareArn(),
		)
	}
	return ""
}

func compareAzureTransitGatewayAttachment(
	spec konnectv1alpha1.AzureVNETPeeringAttachmentConfig,
	remote sdkkonnectcomp.AzureVNETPeeringAttachmentConfig,
) string {
	if spec.TenantID != remote.GetTenantID() {
		return fmt.Sprintf("attachment.tenant_id mismatch spec=%q konnect=%q", spec.TenantID, remote.GetTenantID())
	}
	if spec.SubscriptionID != remote.GetSubscriptionID() {
		return fmt.Sprintf(
			"attachment.subscription_id mismatch spec=%q konnect=%q",
			spec.SubscriptionID,
			remote.GetSubscriptionID(),
		)
	}
	if spec.ResourceGroupName != remote.GetResourceGroupName() {
		return fmt.Sprintf(
			"attachment.resource_group_name mismatch spec=%q konnect=%q",
			spec.ResourceGroupName,
			remote.GetResourceGroupName(),
		)
	}
	if spec.VnetName != remote.GetVnetName() {
		return fmt.Sprintf("attachment.vnet_name mismatch spec=%q konnect=%q", spec.VnetName, remote.GetVnetName())
	}
	return ""
}

func normalizeDNSConfigs(spec []konnectv1alpha1.TransitGatewayDNSConfig) []normalizedDNSConfig {
	result := make([]normalizedDNSConfig, 0, len(spec))
	for _, cfg := range spec {
		result = append(result, normalizedDNSConfig{
			RemoteDNSServerIPAddresses: sortedCopy(cfg.RemoteDNSServerIPAddresses),
			DomainProxyList:            sortedCopy(cfg.DomainProxyList),
		})
	}
	return sortNormalizedDNS(result)
}

func normalizeKonnectDNSConfigs(spec []sdkkonnectcomp.TransitGatewayDNSConfig) []normalizedDNSConfig {
	result := make([]normalizedDNSConfig, 0, len(spec))
	for _, cfg := range spec {
		result = append(result, normalizedDNSConfig{
			RemoteDNSServerIPAddresses: sortedCopy(cfg.GetRemoteDNSServerIPAddresses()),
			DomainProxyList:            sortedCopy(cfg.GetDomainProxyList()),
		})
	}
	return sortNormalizedDNS(result)
}

func marshalNormalized[T any](value T) (string, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func sortedCopy(values []string) []string {
	if values == nil {
		return nil
	}
	copyValues := make([]string, len(values))
	copy(copyValues, values)
	sort.Strings(copyValues)
	return copyValues
}

func sortNormalizedDNS(dns []normalizedDNSConfig) []normalizedDNSConfig {
	if dns == nil {
		return nil
	}
	result := make([]normalizedDNSConfig, len(dns))
	copy(result, dns)
	sort.Slice(result, func(i, j int) bool {
		keyI, _ := marshalNormalized(result[i])
		keyJ, _ := marshalNormalized(result[j])
		return keyI < keyJ
	})
	return result
}
