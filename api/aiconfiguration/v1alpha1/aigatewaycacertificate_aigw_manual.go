package v1alpha1

// This file hand-translates AIGatewayCACertificate into ai-deck-converter's aigw.CACertificate,
// for on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment
// for the shared background and the plan to generate this some day.

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWCACertificate converts the AIGatewayCACertificate into ai-deck-converter's aigw.CACertificate,
// resolving the cert secretRef. It carries no entity references, so unlike e.g. AIGatewaySNI it
// needs no entity-name resolution.
func (obj *AIGatewayCACertificate) ToAIGWCACertificate(ctx context.Context, cl client.Client) (*aigw.CACertificate, error) {
	if obj.Spec.APISpec.Name == "" {
		return nil, fmt.Errorf("AIGatewayCACertificate %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}

	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, fmt.Errorf("AIGatewayCACertificate %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// sdkOpsAPISpec resolves into a copy of the APISpec, so the caller's object is not mutated.
	resolved, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayCACertificate %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}

	data, err := resolved.marshalAIGWCACertificatePayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayCACertificate %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var caCert aigw.CACertificate
	if err := yaml.Unmarshal(data, &caCert); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayCACertificate %s/%s as aigw.CACertificate: %w", obj.Namespace, obj.Name, err)
	}
	if err := validateCACertPEM(caCert.Cert); err != nil {
		return nil, fmt.Errorf("AIGatewayCACertificate %s/%s: invalid cert: %w", obj.Namespace, obj.Name, err)
	}
	return &caCert, nil
}

// validateCACertPEM checks the cert against what Kong's ca_certificates schema rejects at push
// time: an unparsable or expired cert there fails the whole DB-less push, so it is caught here
// instead, turning a bad CR into a per-entity translation error. Bundles with more than one
// certificate are allowed (a legitimate trust-store shape), but every certificate in the bundle
// must parse and be within its validity window.
func validateCACertPEM(cert string) error {
	var (
		rest   = []byte(cert)
		parsed int
		now    = time.Now()
	)
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = next
		if block.Type != "CERTIFICATE" {
			return fmt.Errorf("PEM block of type %q is not a CERTIFICATE", block.Type)
		}
		parsedCert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("parsing certificate: %w", err)
		}
		if now.Before(parsedCert.NotBefore) {
			return fmt.Errorf("certificate %q is not valid before %s", parsedCert.Subject, parsedCert.NotBefore.Format(time.RFC3339))
		}
		if now.After(parsedCert.NotAfter) {
			return fmt.Errorf("certificate %q expired on %s", parsedCert.Subject, parsedCert.NotAfter.Format(time.RFC3339))
		}
		parsed++
	}
	if parsed == 0 {
		return fmt.Errorf("no PEM-encoded CERTIFICATE block found")
	}
	return nil
}

// marshalAIGWCACertificatePayload builds the aigw.CACertificate-shaped payload bytes. Shared by
// ToAIGWCACertificate and its strict round-trip test, so the test decodes the production
// pipeline's output, not a copy of it.
//
// marshalSDKOpsPayload already does everything the aigw shape needs — the sensitive cert union
// flattening and the camel→snake key rename — leaving only the managed_by drop, which needs the
// map round-trip because the pipeline returns bytes.
func (spec *AIGatewayCACertificateAPISpec) marshalAIGWCACertificatePayload() ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayCACertificate SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	return json.Marshal(cfg)
}
