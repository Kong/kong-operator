package v1alpha1

// This file hand-translates AIGatewayPolicy into ai-deck-converter's aigw.Policy, for on-prem
// (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment for the shared
// background and the plan to generate this some day.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWPolicy converts the AIGatewayPolicy into ai-deck-converter's aigw.Policy.
//
// Unlike AIGatewayModel.ToAIGWModel, no CR reference resolution is needed: the datastore
// references are plain names, passed through as-is (there is no AIGatewayDatastore CR yet).
// The client is only used for the config secretRef resolution.
func (obj *AIGatewayPolicy) ToAIGWPolicy(ctx context.Context, cl client.Client) (*aigw.Policy, error) {
	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, fmt.Errorf("AIGatewayPolicy %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// Resolve spec.apiSpec.config's secretRef. sdkOpsAPISpec resolves into a copy of the
	// APISpec (config is a value field), so the caller's object is not mutated.
	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayPolicy %s/%s secrets: %w", obj.Namespace, obj.Name, err)
	}

	data, err := resolved.marshalAIGWPolicyPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayPolicy %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var policy aigw.Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayPolicy %s/%s as aigw.Policy: %w", obj.Namespace, obj.Name, err)
	}
	return &policy, nil
}

// marshalAIGWPolicyPayload builds the aigw.Policy-shaped payload bytes. Shared by ToAIGWPolicy
// and its strict round-trip test, so the test decodes the production pipeline's output, not a
// copy of it.
//
// marshalSDKOpsPayload already does everything the aigw shape needs — freeform config passthrough,
// the Enabled/Disabled string-to-bool normalization for enabled/global — leaving only the
// managed_by drop, which needs the map round-trip because the non-union pipeline returns bytes.
func (spec *AIGatewayPolicyAPISpec) marshalAIGWPolicyPayload() ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayPolicy SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	return json.Marshal(cfg)
}
