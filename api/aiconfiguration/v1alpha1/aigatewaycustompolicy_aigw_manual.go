package v1alpha1

// This file hand-translates AIGatewayCustomPolicy into ai-deck-converter's aigw.CustomPolicy,
// for on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment
// for the shared background and the plan to generate this some day.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWCustomPolicy converts the AIGatewayCustomPolicy into ai-deck-converter's
// aigw.CustomPolicy.
//
// Unlike AIGatewayModel.ToAIGWModel, no CR reference resolution is needed: the custom policy
// is self-contained (its schema/handler come from inline values or same-namespace ConfigMaps,
// already resolved by sdkOpsAPISpec).
func (obj *AIGatewayCustomPolicy) ToAIGWCustomPolicy(ctx context.Context, cl client.Client) (*aigw.CustomPolicy, error) {
	spec := &obj.Spec.APISpec
	if spec.AIGatewayCustomPolicyConfig == nil {
		return nil, fmt.Errorf("AIGatewayCustomPolicy %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}

	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayCustomPolicy %s/%s data sources: %w", obj.Namespace, obj.Name, err)
	}

	data, err := resolved.marshalAIGWCustomPolicyPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayCustomPolicy %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var policy aigw.CustomPolicy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayCustomPolicy %s/%s as aigw.CustomPolicy: %w", obj.Namespace, obj.Name, err)
	}
	return &policy, nil
}

// marshalAIGWCustomPolicyPayload builds the aigw.CustomPolicy-shaped payload bytes. Shared by
// ToAIGWCustomPolicy and its round-trip test, so the test decodes the production pipeline's
// output, not a copy of it.
//
// marshalSDKOpsPayload plus selectedSDKOpsPayload already do everything the aigw shape needs —
// the union flattening lifts the selected variant's name/schema/handler to the top level and
// re-injects the type discriminator — leaving only the drops of the fields aigw.CustomPolicy
// has no equivalent for (managed_by is Konnect-only bookkeeping; display_name and labels have
// no aigw field), which need the map round-trip because the union pipeline returns bytes.
func (spec *AIGatewayCustomPolicyAPISpec) marshalAIGWCustomPolicyPayload() ([]byte, error) {
	payload, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	data, _, err := spec.selectedSDKOpsPayload(payload)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayCustomPolicy SDK payload: %w", err)
	}
	for _, key := range []string{"managed_by", "labels", "display_name"} {
		delete(cfg, key)
	}
	return json.Marshal(cfg)
}
