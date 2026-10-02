package v1alpha1

// This file hand-translates AIGatewayCertificate into ai-deck-converter's aigw.Certificate,
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

// ToAIGWCertificate converts the AIGatewayCertificate into ai-deck-converter's aigw.Certificate,
// resolving the cert/key/certAlt/keyAlt secretRefs. It carries no entity references, so unlike
// e.g. AIGatewaySNI it needs no entity-name resolution.
func (obj *AIGatewayCertificate) ToAIGWCertificate(ctx context.Context, cl client.Client) (*aigw.Certificate, error) {
	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, fmt.Errorf("AIGatewayCertificate %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// Resolve the cert/key/certAlt/keyAlt secretRefs. sdkOpsAPISpec resolves into a copy of the
	// APISpec, so the caller's object is not mutated.
	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayCertificate %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}

	data, err := resolved.marshalAIGWCertificatePayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayCertificate %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var cert aigw.Certificate
	if err := yaml.Unmarshal(data, &cert); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayCertificate %s/%s as aigw.Certificate: %w", obj.Namespace, obj.Name, err)
	}
	return &cert, nil
}

// marshalAIGWCertificatePayload builds the aigw.Certificate-shaped payload bytes. Shared by
// ToAIGWCertificate and its strict round-trip test, so the test decodes the production
// pipeline's output, not a copy of it.
//
// marshalSDKOpsPayload already does everything the aigw shape needs — the sensitive
// cert/key/certAlt/keyAlt union flattening and the camel→snake key rename — leaving only the
// managed_by drop, which needs the map round-trip because the pipeline returns bytes.
func (spec *AIGatewayCertificateAPISpec) marshalAIGWCertificatePayload() ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayCertificate SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	return json.Marshal(cfg)
}
