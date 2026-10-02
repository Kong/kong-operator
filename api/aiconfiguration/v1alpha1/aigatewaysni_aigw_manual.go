package v1alpha1

// This file hand-translates AIGatewaySNI into ai-deck-converter's aigw.SNI,
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

// ToAIGWSNI converts the AIGatewaySNI into ai-deck-converter's aigw.SNI, resolving the
// spec.apiSpec.certificate reference to the referenced certificate's entity name. It carries
// no secretRefs, so unlike AIGatewayCertificate it needs no secret-resolution step.
func (obj *AIGatewaySNI) ToAIGWSNI(ctx context.Context, cl client.Client) (*aigw.SNI, error) {
	data, err := obj.Spec.APISpec.marshalAIGWSNIPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewaySNI %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var sni aigw.SNI
	if err := yaml.Unmarshal(data, &sni); err != nil {
		return nil, fmt.Errorf("decoding AIGatewaySNI %s/%s as aigw.SNI: %w", obj.Namespace, obj.Name, err)
	}

	// aigw.SNI.Certificate is the referenced certificate's entity name: the converter resolves
	// SNIs against the certificates list by name (convertSNIs).
	ref := obj.Spec.APISpec.Certificate
	certName, err := resolveEntityName[AIGatewayCertificate](ctx, cl, obj.Namespace, ref.Namespace, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewaySNI %s/%s certificate: %w", obj.Namespace, obj.Name, err)
	}
	sni.Certificate = certName
	return &sni, nil
}

// marshalAIGWSNIPayload builds the aigw.SNI-shaped payload bytes. Shared by ToAIGWSNI and its
// strict round-trip test, so the test decodes the production pipeline's output, not a copy of it.
func (spec *AIGatewaySNIAPISpec) marshalAIGWSNIPayload() ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewaySNI SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	// Carries the {kind,name,namespace} CR reference; aigw wants the resolved certificate's
	// entity name. Stripped here, re-attached resolved in ToAIGWSNI.
	delete(cfg, "certificate")
	return json.Marshal(cfg)
}
