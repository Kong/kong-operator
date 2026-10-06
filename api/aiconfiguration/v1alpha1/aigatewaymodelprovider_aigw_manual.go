package v1alpha1

// This file hand-translates AIGatewayModelProvider into ai-deck-converter's aigw.Provider,
// for on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment
// for the shared background and the plan to generate this some day.

import (
	"context"
	"fmt"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWProvider converts the AIGatewayModelProvider into ai-deck-converter's aigw.Provider.
//
// Unlike AIGatewayModel.ToAIGWModel, no reference resolution is needed: a provider carries
// no cross-entity references (its auth is inline), so the client is only used for secretRef
// resolution.
func (obj *AIGatewayModelProvider) ToAIGWProvider(ctx context.Context, cl client.Client) (*aigw.Provider, error) {
	spec := &obj.Spec.APISpec
	if spec.AIGatewayModelProviderConfig == nil {
		return nil, fmt.Errorf("AIGatewayModelProvider %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}

	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, fmt.Errorf("AIGatewayModelProvider %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// Resolve secretRefs against a copy: sdkOpsAPISpec writes the resolved values back into
	// the spec it walks, which would otherwise leak into the caller's object.
	resolved, err := obj.DeepCopy().sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayModelProvider %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}

	data, err := resolved.marshalAIGWProviderPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayModelProvider %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var provider aigw.Provider
	if err := yaml.Unmarshal(data, &provider); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayModelProvider %s/%s as aigw.Provider: %w", obj.Namespace, obj.Name, err)
	}
	return &provider, nil
}

// marshalAIGWProviderPayload builds the aigw.Provider-shaped payload bytes for the selected
// config variant. Shared by ToAIGWProvider and its strict round-trip test, so the test decodes
// the production pipeline's output, not a copy of it (mirrors the Model side's
// marshalAIGWPayload).
func (spec *AIGatewayModelProviderAPISpec) marshalAIGWProviderPayload() ([]byte, error) {
	payload, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	cfg, ok := payload[string(spec.Type)].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing %q payload", spec.Type)
	}

	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")

	// The selected variant payload already carries the discriminator as its top-level "type"
	// (selectedSDKOpsPayload copies it in), which is exactly aigw.Provider.Type.
	data, _, err := spec.selectedSDKOpsPayload(payload)
	return data, err
}
