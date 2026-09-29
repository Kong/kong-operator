package v1alpha1

// This file hand-translates AIGatewayAuthStrategy into the ai-deck-converter input format, for
// on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment for
// the shared background and the plan to generate this some day.

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MarshalAIGWAuthStrategy resolves the secretRefs and returns the aigw.AuthStrategy-shaped
// payload bytes for the selected config variant (key-auth, openid-connect).
//
// It returns bytes rather than a typed *aigw.AuthStrategy because ai-deck-converter exports no
// AuthStrategy alias (see aigatewaymodel_aigw_manual.go's package comment): the caller bridges
// the bytes through aigw.Parse of a single-key document envelope. Once the alias lands
// upstream, this can grow the typed ToAIGWAuthStrategy the sibling kinds have.
func (obj *AIGatewayAuthStrategy) MarshalAIGWAuthStrategy(ctx context.Context, cl client.Client) ([]byte, error) {
	spec := &obj.Spec.APISpec
	if spec.AIGatewayAuthStrategyConfig == nil {
		return nil, fmt.Errorf("AIGatewayAuthStrategy %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}

	// Resolve secretRefs against a copy: sdkOpsAPISpec writes the resolved values back into
	// the spec it walks, which would otherwise leak into the caller's object.
	resolved, err := obj.DeepCopy().sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayAuthStrategy %s/%s secrets: %w", obj.Namespace, obj.Name, err)
	}
	return resolved.marshalAIGWAuthStrategyPayload()
}

// marshalAIGWAuthStrategyPayload builds the aigw.AuthStrategy-shaped payload bytes for the
// selected config variant. Shared by MarshalAIGWAuthStrategy and the strict round-trip test, so
// the test decodes the production pipeline's output, not a copy of it.
func (spec *AIGatewayAuthStrategyAPISpec) marshalAIGWAuthStrategyPayload() ([]byte, error) {
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
	// (selectedSDKOpsPayload copies it in); that value ("key-auth", "openid-connect") is both
	// the aigw.AuthStrategy.Type and the Kong plugin name the converter instantiates from it.
	data, _, err := spec.selectedSDKOpsPayload(payload)
	return data, err
}
