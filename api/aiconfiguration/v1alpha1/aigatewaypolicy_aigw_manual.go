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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWPolicy converts the AIGatewayPolicy into ai-deck-converter's aigw.Policy.
//
// The client is used for the config secretRef resolution and for resolving
// spec.apiSpec.customPolicyRef into the referenced custom policy's Konnect
// name (which is sent as the policy's type, per the CRD field's contract).
func (obj *AIGatewayPolicy) ToAIGWPolicy(ctx context.Context, cl client.Client) (*aigw.Policy, error) {
	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, fmt.Errorf("AIGatewayPolicy %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// Resolve spec.apiSpec.config's secretRef. sdkOpsAPISpec resolves into a copy of the
	// APISpec (config is a value field), so the caller's object is not mutated.
	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayPolicy %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}

	data, err := resolved.marshalAIGWPolicyPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayPolicy %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var policy aigw.Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayPolicy %s/%s as aigw.Policy: %w", obj.Namespace, obj.Name, err)
	}

	if ref := obj.Spec.APISpec.CustomPolicyRef; ref.Name != "" {
		if policy.Type != "" {
			return nil, fmt.Errorf("AIGatewayPolicy %s/%s: type and customPolicyRef are mutually exclusive", obj.Namespace, obj.Name)
		}
		if ref.Namespace != "" && ref.Namespace != obj.Namespace {
			return nil, fmt.Errorf("AIGatewayPolicy %s/%s: cross-namespace customPolicyRef %s/%s is not supported on-prem", obj.Namespace, obj.Name, ref.Namespace, ref.Name)
		}
		var customPolicy AIGatewayCustomPolicy
		nn := types.NamespacedName{Namespace: obj.Namespace, Name: ref.Name}
		if err := cl.Get(ctx, nn, &customPolicy); err != nil {
			return nil, fmt.Errorf("resolving AIGatewayPolicy %s/%s customPolicyRef %q: %w", obj.Namespace, obj.Name, ref.Name, err)
		}
		// A custom policy bound to a different AI Gateway is not part of this
		// gateway's rendered document, so referencing it would push a plugin
		// with no custom_plugins entry behind it.
		name := customPolicy.GetKonnectName()
		if name == "" {
			return nil, fmt.Errorf("AIGatewayPolicy %s/%s: referenced custom policy %q has no name set", obj.Namespace, obj.Name, ref.Name)
		}
		if cpGW := customPolicy.Spec.AIGatewayRef; cpGW.NamespacedRef == nil || obj.Spec.AIGatewayRef.NamespacedRef == nil ||
			cpGW.NamespacedRef.Name != obj.Spec.AIGatewayRef.NamespacedRef.Name {
			return nil, fmt.Errorf("AIGatewayPolicy %s/%s: customPolicyRef %q belongs to a different AI Gateway", obj.Namespace, obj.Name, ref.Name)
		}
		policy.Type = name
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
