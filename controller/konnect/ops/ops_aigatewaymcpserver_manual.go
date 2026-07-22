package ops

import (
	"context"
	"encoding/json"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func getAIGatewayMCPServerForUID(
	ctx context.Context,
	sdk sdkkonnectgo.AIGatewayMCPServersSDK,
	obj *konnectv1alpha1.AIGatewayMCPServer,
) (string, error) {
	gatewayID := obj.GetGatewayID()
	if gatewayID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "KonnectAIGateway", Op: GetOp}
	}

	resp, err := sdk.ListAiGatewayMcpServers(ctx, sdkkonnectops.ListAiGatewayMcpServersRequest{
		GatewayID: gatewayID,
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
	}
	if resp == nil || resp.ListAIGatewayMCPServersResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), ErrNilResponse)
	}

	targetType, targetName, ok := getAIGatewayMCPServerSpecLookupKey(obj)
	if !ok {
		return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
	}

	targetUID := string(obj.GetUID())
	for _, entry := range resp.ListAIGatewayMCPServersResponse.Data {
		entryUID, entryType, entryName, entryID, err := getAIGatewayMCPServerResponseLookupKey(&entry)
		if err != nil {
			return "", fmt.Errorf("failed inspecting %s list entry: %w", obj.GetTypeName(), err)
		}
		if entryID == "" {
			continue
		}
		if entryUID != "" && entryUID == targetUID {
			return entryID, nil
		}
		if entryType == targetType && entryName == targetName {
			return entryID, nil
		}
	}
	return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
}

// getAIGatewayMCPServerSpecLookupKey returns the discriminator type and name
// configured on the Kubernetes object's spec, to be matched against the
// Konnect list response.
func getAIGatewayMCPServerSpecLookupKey(obj *konnectv1alpha1.AIGatewayMCPServer) (variantType, name string, ok bool) {
	if obj == nil || obj.Spec.APISpec.AIGatewayMCPServerConfig == nil {
		return "", "", false
	}

	switch obj.Spec.APISpec.Type {
	case konnectv1alpha1.AIGatewayMCPServerConfigTypeConversionOnly:
		if obj.Spec.APISpec.ConversionOnly == nil {
			return "", "", false
		}
		return string(konnectv1alpha1.AIGatewayMCPServerConfigTypeConversionOnly), string(obj.Spec.APISpec.ConversionOnly.Name), true
	case konnectv1alpha1.AIGatewayMCPServerConfigTypeConversionListener:
		if obj.Spec.APISpec.ConversionListener == nil {
			return "", "", false
		}
		return string(konnectv1alpha1.AIGatewayMCPServerConfigTypeConversionListener), string(obj.Spec.APISpec.ConversionListener.Name), true
	case konnectv1alpha1.AIGatewayMCPServerConfigTypeListener:
		if obj.Spec.APISpec.Listener == nil {
			return "", "", false
		}
		return string(konnectv1alpha1.AIGatewayMCPServerConfigTypeListener), string(obj.Spec.APISpec.Listener.Name), true
	case konnectv1alpha1.AIGatewayMCPServerConfigTypePassthroughListener:
		if obj.Spec.APISpec.PassthroughListener == nil {
			return "", "", false
		}
		return string(konnectv1alpha1.AIGatewayMCPServerConfigTypePassthroughListener), string(obj.Spec.APISpec.PassthroughListener.Name), true
	case konnectv1alpha1.AIGatewayMCPServerConfigTypeUpstreamServer:
		if obj.Spec.APISpec.UpstreamServer == nil {
			return "", "", false
		}
		return string(konnectv1alpha1.AIGatewayMCPServerConfigTypeUpstreamServer), string(obj.Spec.APISpec.UpstreamServer.Name), true
	default:
		return "", "", false
	}
}

// getAIGatewayMCPServerResponseLookupKey extracts the Kubernetes UID label,
// discriminator type, name, and Konnect ID from a list response entry.
//
// The response entry is a two-level discriminated union (outer deployment
// mode, inner ACL attribute type) with no shared accessor across variants.
// Its MarshalJSON already flattens every level down to the real API JSON
// shape (the outer "type" and the leaf's "id"/"name"/"labels" all end up in
// the same flat object), so a JSON round-trip reads all four fields at once
// regardless of which variant is set.
func getAIGatewayMCPServerResponseLookupKey(entry *sdkkonnectcomp.AIGatewayMCPServer) (uid, variantType, name, id string, err error) {
	if entry == nil {
		return "", "", "", "", nil
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return "", "", "", "", err
	}
	var probe struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Type   string            `json:"type"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", "", "", "", err
	}
	return probe.Labels[KubernetesUIDLabelKey], probe.Type, probe.Name, probe.ID, nil
}
