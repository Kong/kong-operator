package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func getAIGatewayModelForUID(
	ctx context.Context,
	sdk sdkkonnectgo.AIGatewayModelsSDK,
	obj *konnectv1alpha1.AIGatewayModel,
) (string, error) {
	gatewayID := obj.GetGatewayID()
	if gatewayID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "AIGatewayControlPlane", Op: GetOp}
	}

	resp, err := sdk.ListAiGatewayModels(ctx, sdkkonnectops.ListAiGatewayModelsRequest{
		GatewayID: gatewayID,
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
	}
	if resp == nil || resp.ListAIGatewayModelsResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), ErrNilResponse)
	}

	targetType, targetName, ok := getAIGatewayModelSpecLookupKey(obj)
	if !ok {
		return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
	}

	targetUID := string(obj.GetUID())
	for _, entry := range resp.ListAIGatewayModelsResponse.Data {
		entryUID, entryType, entryName, entryID := getAIGatewayModelResponseLookupKey(&entry)
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

func getAIGatewayModelSpecLookupKey(obj *konnectv1alpha1.AIGatewayModel) (variantType, name string, ok bool) {
	if obj == nil || obj.Spec.APISpec.AIGatewayModelConfig == nil {
		return "", "", false
	}

	switch obj.Spec.APISpec.Type {
	case konnectv1alpha1.AIGatewayModelConfigTypeAPI:
		if obj.Spec.APISpec.API == nil {
			return "", "", false
		}
		return string(konnectv1alpha1.AIGatewayModelConfigTypeAPI), string(obj.Spec.APISpec.API.Name), true
	case konnectv1alpha1.AIGatewayModelConfigTypeModel:
		if obj.Spec.APISpec.Model == nil {
			return "", "", false
		}
		return string(konnectv1alpha1.AIGatewayModelConfigTypeModel), string(obj.Spec.APISpec.Model.Name), true
	default:
		return "", "", false
	}
}

func getAIGatewayModelResponseLookupKey(entry *sdkkonnectcomp.AIGatewayModel) (uid, variantType, name, id string) {
	if entry == nil {
		return "", "", "", ""
	}

	if variant := entry.AIGatewayModelAIGatewayModelAPI; variant != nil {
		return variant.GetLabels()[KubernetesUIDLabelKey], string(sdkkonnectcomp.AIGatewayModelTypeAPI), variant.GetName(), variant.GetID()
	}
	if variant := entry.AIGatewayModelAIGatewayModelModel; variant != nil {
		return variant.GetLabels()[KubernetesUIDLabelKey], string(sdkkonnectcomp.AIGatewayModelTypeModel), variant.GetName(), variant.GetID()
	}
	return "", "", "", ""
}
