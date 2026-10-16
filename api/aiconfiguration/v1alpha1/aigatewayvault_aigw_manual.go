package v1alpha1

// This file hand-translates AIGatewayVault into ai-deck-converter's aigw.Vault, for on-prem
// (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment for the
// shared background and the plan to generate this some day.

import (
	"context"
	"fmt"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWVault converts the AIGatewayVault into ai-deck-converter's aigw.Vault. It carries no
// entity references, so unlike e.g. AIGatewaySNI it needs no entity-name resolution.
func (obj *AIGatewayVault) ToAIGWVault(ctx context.Context, cl client.Client) (*aigw.Vault, error) {
	if obj.Spec.APISpec.AIGatewayVaultConfig == nil {
		return nil, fmt.Errorf("AIGatewayVault %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}
	// A Konnect Config Store vault resolves its secrets from Konnect, which a self-managed
	// gateway cannot reach: the backend does not exist there, so the whole db-less push
	// would be rejected and take every other entity down with it. Reject it per-entity
	// instead, the same way an unresolvable secretRef is rejected.
	if obj.Spec.APISpec.Type == AIGatewayVaultConfigTypeKonnectConfigStore {
		return nil, fmt.Errorf(
			"AIGatewayVault %s/%s: spec.apiSpec.type %q is not supported on-prem: a Konnect Config Store has no self-managed gateway equivalent",
			obj.Namespace, obj.Name, AIGatewayVaultConfigTypeKonnectConfigStore,
		)
	}

	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, fmt.Errorf("AIGatewayVault %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// sdkOpsAPISpec resolves into a copy of the APISpec, so the caller's object is not mutated.
	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayVault %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}

	data, err := resolved.marshalAIGWVaultPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayVault %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var vault aigw.Vault
	if err := yaml.Unmarshal(data, &vault); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayVault %s/%s as aigw.Vault: %w", obj.Namespace, obj.Name, err)
	}
	return &vault, nil
}

// marshalAIGWVaultPayload builds the aigw.Vault-shaped payload bytes. Shared by ToAIGWVault
// and its strict round-trip test, so the test decodes the production pipeline's output, not
// a copy of it.
//
// selectedSDKOpsPayload already does everything the aigw shape needs — selecting the variant
// named by spec.apiSpec.type, flattening its nested unions (e.g. hcv's authMethod + token
// into a flat config), and hoisting the backend name into the payload's `type` — leaving only
// the managed_by drop, which edits the selected variant in the payload before it is marshaled.
// A variant named by spec.apiSpec.type but absent from the payload needs no check here: the
// selector below reports it.
func (spec *AIGatewayVaultAPISpec) marshalAIGWVaultPayload() ([]byte, error) {
	payload, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	// The variant keys are the spec.apiSpec.type values, and a nil config carries no type to
	// read: the selector below reports that case.
	if spec.AIGatewayVaultConfig != nil {
		if variant, ok := payload[string(spec.Type)].(map[string]any); ok {
			// Konnect-only bookkeeping: no on-prem equivalent.
			delete(variant, "managed_by")
		}
	}
	data, _, err := spec.selectedSDKOpsPayload(payload)
	return data, err
}
