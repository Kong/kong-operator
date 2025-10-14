package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	kcfgkonnect "github.com/kong/kong-operator/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

// AdoptMatch performs adoption of a Konnect entity in "match" mode. When successful, the existing
// entity in Konnect is managed by the operator without modifying it.
func AdoptMatch[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	sdk sdkops.SDKWrapper,
	cl client.Client,
	ent TEnt,
) error {
	adoptable, ok := any(ent).(constraints.KonnectEntityTypeSupportingAdoption)
	if !ok {
		err := fmt.Errorf("%T does not support adoption", ent)
		SetKonnectEntityAdoptedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		return err
	}

	adoptOpts := adoptable.GetAdoptOptions()
	if adoptOpts == nil {
		err := fmt.Errorf("adopt options must be provided")
		SetKonnectEntityAdoptedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		return err
	}

	if adoptOpts.From != commonv1alpha1.AdoptSourceKonnect {
		err := fmt.Errorf("unsupported adopt source %q", adoptOpts.From)
		SetKonnectEntityAdoptedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		return err
	}

	if adoptOpts.Konnect == nil || adoptOpts.Konnect.ID == "" {
		err := fmt.Errorf("konnect ID must be provided for adoption")
		SetKonnectEntityAdoptedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		return err
	}

	mode := adoptOpts.Mode
	if mode == "" {
		mode = commonv1alpha1.AdoptModeMatch
	}
	if mode != commonv1alpha1.AdoptModeMatch {
		err := fmt.Errorf("unsupported adopt mode %q", mode)
		SetKonnectEntityAdoptedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		return err
	}

	var err error
	switch obj := any(ent).(type) {
	case *konnectv1alpha1.KonnectCloudGatewayNetwork:
		err = adoptKonnectCloudGatewayNetworkMatch(ctx, sdk.GetCloudGatewaysSDK(), obj, adoptOpts.Konnect.ID)
	case *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration:
		err = adoptKonnectDataPlaneGroupConfigurationMatch(ctx, sdk.GetCloudGatewaysSDK(), cl, obj, adoptOpts.Konnect.ID)
	case *konnectv1alpha1.KonnectCloudGatewayTransitGateway:
		err = adoptKonnectTransitGatewayMatch(ctx, sdk.GetCloudGatewaysSDK(), obj, adoptOpts.Konnect.ID)
	default:
		err = fmt.Errorf("unsupported adopt target type %T", ent)
	}

	if err != nil {
		var errFetch KonnectEntityAdoptionFetchError
		var errNotMatch KonnectEntityAdoptionNotMatchError
		switch {
		case errors.As(err, &errFetch):
			SetKonnectEntityAdoptedConditionFalse(ent, konnectv1alpha1.KonnectEntityAdoptedReasonFetchFailed, err)
			SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		case errors.As(err, &errNotMatch):
			SetKonnectEntityAdoptedConditionFalse(ent, konnectv1alpha1.KonnectEntityAdoptedReasonNotMatch, err)
			SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		default:
			SetKonnectEntityAdoptedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
			SetKonnectEntityProgrammedConditionFalse(ent, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		}
		return err
	}

	SetKonnectEntityAdoptedConditionTrue(ent)
	SetKonnectEntityProgrammedConditionTrue(ent)
	return nil
}

func adoptKonnectCloudGatewayNetworkMatch(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	n *konnectv1alpha1.KonnectCloudGatewayNetwork,
	konnectID string,
) error {
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
	if !equalStringSets(spec.AvailabilityZones, remote.GetAvailabilityZones()) {
		diffs = append(diffs, fmt.Sprintf(
			"availability_zones spec=%v konnect=%v",
			sortedCopy(spec.AvailabilityZones),
			sortedCopy(remote.GetAvailabilityZones()),
		))
	}
	if spec.CidrBlock != remote.GetCidrBlock() {
		diffs = append(diffs, fmt.Sprintf("cidr_block spec=%q konnect=%q", spec.CidrBlock, remote.GetCidrBlock()))
	}
	return diffs
}

func adoptKonnectDataPlaneGroupConfigurationMatch(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	cl client.Client,
	cfg *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
	konnectID string,
) error {
	cpID := cfg.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cfg, Op: GetOp}
	}

	resp, err := sdk.GetConfiguration(ctx, konnectID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{KonnectID: konnectID, Err: err}
	}
	if resp == nil || resp.ConfigurationManifest == nil {
		return fmt.Errorf("failed getting %s: %w", cfg.GetTypeName(), ErrNilResponse)
	}

	manifest := resp.ConfigurationManifest

	if diff, err := compareDataPlaneGroupConfigurationSpec(ctx, cl, cfg, manifest); err != nil {
		return err
	} else if diff != "" {
		return KonnectEntityAdoptionNotMatchError{KonnectID: konnectID}
	}

	cfg.SetKonnectID(manifest.ID)
	cfg.Status.DataPlaneGroups = dataPlaneGroupsResponseToStatus(manifest.GetDataplaneGroups())
	return nil
}

func compareDataPlaneGroupConfigurationSpec(
	ctx context.Context,
	cl client.Client,
	cfg *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
	manifest *sdkkonnectcomp.ConfigurationManifest,
) (string, error) {
	if manifest.GetControlPlaneID() != cfg.GetControlPlaneID() {
		return fmt.Sprintf(
			"controlPlaneRef mismatch spec=%q konnect=%q",
			cfg.GetControlPlaneID(),
			manifest.GetControlPlaneID(),
		), nil
	}

	specAPIAccess := sdkkonnectcomp.APIAccessPrivatePlusPublic
	if cfg.Spec.APIAccess != nil {
		specAPIAccess = *cfg.Spec.APIAccess
	}
	manifestAPIAccess := sdkkonnectcomp.APIAccessPrivatePlusPublic
	if manifest.GetAPIAccess() != nil {
		manifestAPIAccess = *manifest.GetAPIAccess()
	}
	if specAPIAccess != manifestAPIAccess {
		return fmt.Sprintf(
			"api_access mismatch spec=%q konnect=%q",
			specAPIAccess,
			manifestAPIAccess,
		), nil
	}

	if cfg.Spec.Version != manifest.GetVersion() {
		return fmt.Sprintf("version mismatch spec=%q konnect=%q", cfg.Spec.Version, manifest.GetVersion()), nil
	}

	specGroups, err := normalizeSpecDataplaneGroups(ctx, cl, cfg)
	if err != nil {
		return "", err
	}
	manifestGroups, err := normalizeKonnectDataplaneGroups(manifest.GetDataplaneGroups())
	if err != nil {
		return "", err
	}

	specJSON, err := marshalNormalized(specGroups)
	if err != nil {
		return "", err
	}
	manifestJSON, err := marshalNormalized(manifestGroups)
	if err != nil {
		return "", err
	}

	if specJSON != manifestJSON {
		return fmt.Sprintf("dataplane_groups mismatch spec=%s konnect=%s", specJSON, manifestJSON), nil
	}

	return "", nil
}

func adoptKonnectTransitGatewayMatch(
	ctx context.Context,
	sdk sdkops.CloudGatewaysSDK,
	tg *konnectv1alpha1.KonnectCloudGatewayTransitGateway,
	konnectID string,
) error {
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
		if !equalStringSets(spec.AWSTransitGateway.CIDRBlocks, aws.GetCidrBlocks()) {
			return fmt.Sprintf(
				"cidr_blocks mismatch spec=%v konnect=%v",
				sortedCopy(spec.AWSTransitGateway.CIDRBlocks),
				sortedCopy(aws.GetCidrBlocks()),
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

func normalizeSpecDataplaneGroups(
	ctx context.Context,
	cl client.Client,
	cfg *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration,
) ([]normalizedDPGroup, error) {
	result := make([]normalizedDPGroup, 0, len(cfg.Spec.DataplaneGroups))
	for _, group := range cfg.Spec.DataplaneGroups {
		networkID, err := resolveNetworkRef(ctx, cl, cfg.GetNamespace(), group.NetworkRef)
		if err != nil {
			return nil, err
		}

		norm := normalizedDPGroup{
			Provider:    string(group.Provider),
			Region:      group.Region,
			NetworkID:   networkID,
			Autoscale:   normalizeSpecAutoscale(group.Autoscale),
			Environment: normalizeSpecEnvironment(group.Environment),
		}
		result = append(result, norm)
	}
	return sortNormalizedDPGroups(result)
}

func normalizeKonnectDataplaneGroups(groups []sdkkonnectcomp.ConfigurationDataPlaneGroup) ([]normalizedDPGroup, error) {
	result := make([]normalizedDPGroup, 0, len(groups))
	for _, group := range groups {
		norm := normalizedDPGroup{
			Provider:    string(group.GetProvider()),
			Region:      group.GetRegion(),
			NetworkID:   group.GetCloudGatewayNetworkID(),
			Environment: normalizeKonnectEnvironment(group.GetEnvironment()),
		}

		autoNorm, err := normalizeKonnectAutoscale(group.GetAutoscale())
		if err != nil {
			return nil, err
		}
		norm.Autoscale = autoNorm
		result = append(result, norm)
	}
	return sortNormalizedDPGroups(result)
}

func normalizeSpecAutoscale(autoscale konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale) normalizedAutoscale {
	switch autoscale.Type {
	case konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot:
		norm := normalizedAutoscale{Type: string(konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot)}
		if autoscale.Autopilot != nil {
			norm.Autopilot = &normalizedAutopilot{
				BaseRps: autoscale.Autopilot.BaseRps,
				MaxRps:  autoscale.Autopilot.MaxRps,
			}
		}
		return norm
	case konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic:
		norm := normalizedAutoscale{Type: string(konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic)}
		if autoscale.Static != nil {
			norm.Static = &normalizedStatic{
				InstanceType:       string(autoscale.Static.InstanceType),
				RequestedInstances: autoscale.Static.RequestedInstances,
			}
		}
		return norm
	default:
		return normalizedAutoscale{Type: string(autoscale.Type)}
	}
}

func normalizeKonnectAutoscale(autoscale sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscale) (normalizedAutoscale, error) {
	if autoscale.ConfigurationDataPlaneGroupAutoscaleAutopilot != nil {
		auto := autoscale.ConfigurationDataPlaneGroupAutoscaleAutopilot
		return normalizedAutoscale{
			Type: string(konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot),
			Autopilot: &normalizedAutopilot{
				BaseRps: auto.GetBaseRps(),
				MaxRps:  auto.GetMaxRps(),
			},
		}, nil
	}
	if autoscale.ConfigurationDataPlaneGroupAutoscaleStatic != nil {
		st := autoscale.ConfigurationDataPlaneGroupAutoscaleStatic
		return normalizedAutoscale{
			Type: string(konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic),
			Static: &normalizedStatic{
				InstanceType:       string(st.GetInstanceType()),
				RequestedInstances: st.GetRequestedInstances(),
			},
		}, nil
	}
	return normalizedAutoscale{}, fmt.Errorf("unsupported autoscale configuration in Konnect response")
}

func normalizeSpecEnvironment(env []konnectv1alpha1.ConfigurationDataPlaneGroupEnvironmentField) []normalizedEnvironment {
	result := make([]normalizedEnvironment, 0, len(env))
	for _, e := range env {
		result = append(result, normalizedEnvironment{Name: e.Name, Value: e.Value})
	}
	return sortNormalizedEnvironment(result)
}

func normalizeKonnectEnvironment(env []sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField) []normalizedEnvironment {
	result := make([]normalizedEnvironment, 0, len(env))
	for _, e := range env {
		result = append(result, normalizedEnvironment{Name: e.GetName(), Value: e.GetValue()})
	}
	return sortNormalizedEnvironment(result)
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

func resolveNetworkRef(
	ctx context.Context,
	cl client.Client,
	namespace string,
	ref commonv1alpha1.ObjectRef,
) (string, error) {
	switch ref.Type {
	case commonv1alpha1.ObjectRefTypeKonnectID:
		return *ref.KonnectID, nil
	case commonv1alpha1.ObjectRefTypeNamespacedRef:
		if ref.NamespacedRef == nil {
			return "", fmt.Errorf("namespacedRef must be provided for networkRef")
		}
		var network konnectv1alpha1.KonnectCloudGatewayNetwork
		key := types.NamespacedName{Namespace: namespace, Name: ref.NamespacedRef.Name}
		if err := cl.Get(ctx, key, &network); err != nil {
			return "", fmt.Errorf("failed to get KonnectCloudGatewayNetwork %s: %w", key, err)
		}
		if network.GetKonnectID() == "" {
			return "", fmt.Errorf("KonnectCloudGatewayNetwork %s does not have a Konnect ID", key)
		}
		return network.GetKonnectID(), nil
	default:
		return "", fmt.Errorf("unsupported network ref type %q", ref.Type)
	}
}

func equalStringSets(a, b []string) bool {
	return slices.Equal(sortedCopy(a), sortedCopy(b))
}

func sortedCopy(values []string) []string {
	copyValues := slices.Clone(values)
	sort.Strings(copyValues)
	return copyValues
}

func marshalNormalized[T any](value T) (string, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func sortNormalizedDPGroups(groups []normalizedDPGroup) ([]normalizedDPGroup, error) {
	type sortableGroup struct {
		key string
		val normalizedDPGroup
	}
	s := make([]sortableGroup, 0, len(groups))
	for _, g := range groups {
		envSorted := sortNormalizedEnvironment(g.Environment)
		auto := g.Autoscale
		if auto.Static != nil {
			auto.Static = &normalizedStatic{
				InstanceType:       auto.Static.InstanceType,
				RequestedInstances: auto.Static.RequestedInstances,
			}
		}
		if auto.Autopilot != nil {
			auto.Autopilot = &normalizedAutopilot{
				BaseRps: auto.Autopilot.BaseRps,
				MaxRps:  auto.Autopilot.MaxRps,
			}
		}
		g.Environment = envSorted
		g.Autoscale = auto
		key, err := marshalNormalized(g)
		if err != nil {
			return nil, err
		}
		s = append(s, sortableGroup{key: key, val: g})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].key < s[j].key })
	result := make([]normalizedDPGroup, 0, len(s))
	for _, entry := range s {
		result = append(result, entry.val)
	}
	return result, nil
}

func sortNormalizedEnvironment(env []normalizedEnvironment) []normalizedEnvironment {
	result := slices.Clone(env)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].Value < result[j].Value
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func sortNormalizedDNS(dns []normalizedDNSConfig) []normalizedDNSConfig {
	result := slices.Clone(dns)
	sort.Slice(result, func(i, j int) bool {
		keyI, _ := marshalNormalized(result[i])
		keyJ, _ := marshalNormalized(result[j])
		return keyI < keyJ
	})
	return result
}

type normalizedAutoscale struct {
	Type      string               `json:"type"`
	Static    *normalizedStatic    `json:"static,omitempty"`
	Autopilot *normalizedAutopilot `json:"autopilot,omitempty"`
}

type normalizedStatic struct {
	InstanceType       string `json:"instanceType"`
	RequestedInstances int64  `json:"requestedInstances"`
}

type normalizedAutopilot struct {
	BaseRps int64  `json:"baseRps"`
	MaxRps  *int64 `json:"maxRps,omitempty"`
}

type normalizedEnvironment struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type normalizedDPGroup struct {
	Provider    string                  `json:"provider"`
	Region      string                  `json:"region"`
	NetworkID   string                  `json:"networkID"`
	Autoscale   normalizedAutoscale     `json:"autoscale"`
	Environment []normalizedEnvironment `json:"environment,omitempty"`
}

type normalizedDNSConfig struct {
	RemoteDNSServerIPAddresses []string `json:"remote_dns_server_ip_addresses,omitempty"`
	DomainProxyList            []string `json:"domain_proxy_list,omitempty"`
}
