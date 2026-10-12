package v1alpha1

// This file hand-translates AIGatewayAgent into ai-deck-converter's aigw.Agent,
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

// ToAIGWAgent converts the AIGatewayAgent into ai-deck-converter's aigw.Agent, resolving
// spec.apiSpec.policies and spec.apiSpec.access (ACL consumer groups and auth strategies)
// references to the referenced entities' names.
//
// It carries no secretRefs, so unlike e.g. AIGatewayConsumer it needs no secret resolution.
func (obj *AIGatewayAgent) ToAIGWAgent(ctx context.Context, cl client.Client) (*aigw.Agent, error) {
	data, err := obj.Spec.APISpec.marshalAIGWAgentPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayAgent %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var agent aigw.Agent
	if err := yaml.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayAgent %s/%s as aigw.Agent: %w", obj.Namespace, obj.Name, err)
	}

	if agent.Policies, err = resolveEntityNames[AIGatewayPolicy](ctx, cl, obj.Namespace, policyRefs(obj.Spec.APISpec.Policies)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayAgent %s/%s policies: %w", obj.Namespace, obj.Name, err)
	}

	if err := obj.resolveAIGWAccess(ctx, cl, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// resolveAIGWAccess resolves the agent's access.acls and access.authStrategies CR references
// onto the translated aigw.Agent's access.
//
// aigw.AgentAccessConfig.UnmarshalYAML already folded the payload's deprecated
// identity_providers key (plain strings, no CR references) into Access.AuthStrategies;
// current-key refs come first, mirroring AIGatewayModel's ordering.
func (obj *AIGatewayAgent) resolveAIGWAccess(
	ctx context.Context, cl client.Client, agent *aigw.Agent,
) error {
	spec := &obj.Spec.APISpec

	if acls := spec.Access.Acls; acls != nil {
		var (
			aclRefs []AIGatewayACLRef
			target  *[]string
		)
		switch acls.Type {
		case AIGatewayAgentAccessAclsTypeAllow:
			if acls.Allow != nil {
				aclRefs = acls.Allow.Allow
			}
			target = &agent.Access.ACLs.Allow
		case AIGatewayAgentAccessAclsTypeDeny:
			if acls.Deny != nil {
				aclRefs = acls.Deny.Deny
			}
			target = &agent.Access.ACLs.Deny
		default:
			return fmt.Errorf("AIGatewayAgent %s/%s: unsupported access.acls.type %q", obj.Namespace, obj.Name, acls.Type)
		}
		names, err := resolveEntityNames[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, consumerGroupRefs(aclRefs))
		if err != nil {
			return fmt.Errorf("resolving AIGatewayAgent %s/%s access.acls: %w", obj.Namespace, obj.Name, err)
		}
		*target = names
	}

	authStrategies, err := resolveEntityNames[AIGatewayAuthStrategy](ctx, cl, obj.Namespace, authStrategyRefs(spec.Access.AuthStrategies))
	if err != nil {
		return fmt.Errorf("resolving AIGatewayAgent %s/%s access.authStrategies: %w", obj.Namespace, obj.Name, err)
	}
	agent.Access.AuthStrategies = append(authStrategies, agent.Access.AuthStrategies...)
	return nil
}

// marshalAIGWAgentPayload builds the aigw.Agent-shaped payload bytes. Shared by ToAIGWAgent
// and its strict round-trip test, so the test decodes the production pipeline's output, not a
// copy of it.
//
// marshalSDKOpsPayload already does everything the aigw shape needs — the Enabled/Disabled
// boolean enum normalization and the camel→snake key rename — leaving only the managed_by
// drop (Konnect-only bookkeeping) and the stripping of the CR-reference keys
// (policies, access.acls, access.auth_strategies: they carry {kind,name} references, aigw
// wants plain resolved names, re-attached in ToAIGWAgent). The deprecated
// access.identity_providers stays: it carries plain strings with no CR references, and
// aigw.AgentAccessConfig.UnmarshalYAML folds it into AuthStrategies.
func (spec *AIGatewayAgentAPISpec) marshalAIGWAgentPayload() ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayAgent SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	// These carry {kind,name} CR references; aigw wants plain resolved names. Stripped here,
	// re-attached resolved in ToAIGWAgent.
	delete(cfg, "policies")
	if access, ok := cfg["access"].(map[string]any); ok {
		delete(access, "acls")
		delete(access, "auth_strategies")
	}
	return json.Marshal(cfg)
}
