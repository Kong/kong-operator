package v1alpha1

// This file hand-translates AIGatewayAuthStrategy into ai-deck-converter's aigw.AuthStrategy,
// for on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment
// for the shared background and the plan to generate this some day.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWAuthStrategy converts the AIGatewayAuthStrategy into ai-deck-converter's
// aigw.AuthStrategy, resolving secretRefs in the config (e.g. the openid-connect
// clientSecret). It carries no entity references, so unlike the model translation it needs
// no name resolution.
func (obj *AIGatewayAuthStrategy) ToAIGWAuthStrategy(ctx context.Context, cl client.Client) (*aigw.AuthStrategy, error) {
	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, fmt.Errorf("AIGatewayAuthStrategy %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// Resolve the config's secretRefs. sdkOpsAPISpec resolves into a copy of the APISpec,
	// so the caller's object is not mutated.
	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayAuthStrategy %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}

	data, err := marshalAIGWAuthStrategyPayload(resolved)
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayAuthStrategy %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var strategy aigw.AuthStrategy
	if err := yaml.Unmarshal(data, &strategy); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayAuthStrategy %s/%s as aigw.AuthStrategy: %w", obj.Namespace, obj.Name, err)
	}
	return &strategy, nil
}

// marshalAIGWAuthStrategyPayload builds the aigw.AuthStrategy-shaped payload bytes. Shared by
// ToAIGWAuthStrategy and its strict round-trip test, so the test decodes the production
// pipeline's output, not a copy of it.
//
// marshalSDKOpsPayload produces the Konnect SDK shape, which nests the variant's fields under
// the discriminator value (e.g. "key-auth": {name, display_name, config}). aigw.AuthStrategy is
// flat (type, name, display_name, config, labels at the top level), so the variant's fields are
// lifted; the Konnect-only managed_by bookkeeping is dropped on the way.
func marshalAIGWAuthStrategyPayload(spec *AIGatewayAuthStrategyAPISpec) ([]byte, error) {
	payload, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	typeDiscriminator, ok := payload["type"].(string)
	if !ok || typeDiscriminator == "" {
		return nil, fmt.Errorf("spec.apiSpec is required")
	}
	variant, ok := payload[typeDiscriminator].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("AIGatewayAuthStrategy config payload missing for type %q", typeDiscriminator)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(variant, "managed_by")
	cfg := map[string]any{"type": typeDiscriminator}
	maps.Copy(cfg, variant)
	return json.Marshal(cfg)
}
