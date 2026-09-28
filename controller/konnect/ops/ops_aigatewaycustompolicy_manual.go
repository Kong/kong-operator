package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// getAIGatewayCustomPolicyForUID looks up the Konnect ID of an
// AIGatewayCustomPolicy. Custom policy variants carry no labels/tags, so the
// match is made on the discriminator type and the immutable, unique name.
func getAIGatewayCustomPolicyForUID(
	ctx context.Context,
	sdk sdkkonnectgo.AIGatewayCustomPoliciesSDK,
	obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy,
) (string, error) {
	gatewayID := obj.GetGatewayID()
	if gatewayID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "KonnectAIGateway", Op: GetOp}
	}

	resp, err := sdk.ListAiGatewayCustomPolicies(ctx, sdkkonnectops.ListAiGatewayCustomPoliciesRequest{
		GatewayID: gatewayID,
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
	}
	if resp == nil || resp.ListAIGatewayCustomPoliciesResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), ErrNilResponse)
	}

	targetType, targetName, ok := getAIGatewayCustomPolicySpecLookupKey(obj)
	if !ok {
		return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
	}

	// TODO: only the first page of results is scanned. Tracked in
	// https://github.com/Kong/kong-operator/issues/3987.
	for _, entry := range resp.ListAIGatewayCustomPoliciesResponse.Data {
		entryType, entryName, entryID := getAIGatewayCustomPolicyResponseLookupKey(&entry)
		if entryID == "" {
			continue
		}
		if entryType == targetType && entryName == targetName {
			return entryID, nil
		}
	}
	return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
}

// getAIGatewayCustomPolicySpecLookupKey returns the discriminator type and
// name configured on the Kubernetes object's spec, to be matched against the
// Konnect list response.
func getAIGatewayCustomPolicySpecLookupKey(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) (variantType, name string, ok bool) {
	if obj == nil || obj.Spec.APISpec.AIGatewayCustomPolicyConfig == nil {
		return "", "", false
	}

	switch obj.Spec.APISpec.Type {
	case aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeInstalled:
		if obj.Spec.APISpec.Installed == nil {
			return "", "", false
		}
		return string(aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeInstalled), string(obj.Spec.APISpec.Installed.Name), true
	case aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming:
		if obj.Spec.APISpec.Streaming == nil {
			return "", "", false
		}
		return string(aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming), string(obj.Spec.APISpec.Streaming.Name), true
	default:
		return "", "", false
	}
}

// getAIGatewayCustomPolicyResponseLookupKey extracts the discriminator type,
// name, and Konnect ID from a list response entry.
func getAIGatewayCustomPolicyResponseLookupKey(entry *sdkkonnectcomp.AIGatewayCustomPolicy) (variantType, name, id string) {
	if entry == nil {
		return "", "", ""
	}

	if variant := entry.AIGatewayCustomPolicyInstalled; variant != nil {
		return string(sdkkonnectcomp.AIGatewayCustomPolicyTypeInstalled), variant.GetName(), variant.GetID()
	}
	if variant := entry.AIGatewayCustomPolicyStreaming; variant != nil {
		return string(sdkkonnectcomp.AIGatewayCustomPolicyTypeStreaming), variant.GetName(), variant.GetID()
	}
	return "", "", ""
}
