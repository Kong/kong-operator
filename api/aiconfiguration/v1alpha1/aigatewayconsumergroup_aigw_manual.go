package v1alpha1

// This file hand-translates AIGatewayConsumerGroup into ai-deck-converter's aigw.ConsumerGroup,
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

// ToAIGWConsumerGroup converts the AIGatewayConsumerGroup into ai-deck-converter's
// aigw.ConsumerGroup, resolving spec.apiSpec.policies references to the referenced policies'
// entity names. It carries no secretRefs, so unlike the other translations it needs no
// secret-resolution step.
func (obj *AIGatewayConsumerGroup) ToAIGWConsumerGroup(ctx context.Context, cl client.Client) (*aigw.ConsumerGroup, error) {
	data, err := obj.Spec.APISpec.marshalAIGWConsumerGroupPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayConsumerGroup %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var group aigw.ConsumerGroup
	if err := yaml.Unmarshal(data, &group); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayConsumerGroup %s/%s as aigw.ConsumerGroup: %w", obj.Namespace, obj.Name, err)
	}

	if group.Policies, err = resolveEntityNames[AIGatewayPolicy](ctx, cl, obj.Namespace, policyRefs(obj.Spec.APISpec.Policies)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayConsumerGroup %s/%s policies: %w", obj.Namespace, obj.Name, err)
	}
	return &group, nil
}

// marshalAIGWConsumerGroupPayload builds the aigw.ConsumerGroup-shaped payload bytes. Shared by
// ToAIGWConsumerGroup and its strict round-trip test, so the test decodes the production
// pipeline's output, not a copy of it.
func (spec *AIGatewayConsumerGroupAPISpec) marshalAIGWConsumerGroupPayload() ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayConsumerGroup SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	// These carry {kind,name} CR references; aigw wants plain resolved names. Stripped here,
	// re-attached resolved in ToAIGWConsumerGroup.
	delete(cfg, "policies")
	return json.Marshal(cfg)
}
