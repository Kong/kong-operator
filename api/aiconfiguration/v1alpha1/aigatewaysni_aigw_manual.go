package v1alpha1

// This file hand-translates AIGatewaySNI into ai-deck-converter's aigw.SNI,
// for on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment
// for the shared background and the plan to generate this some day.

import (
	"context"
	"encoding/json"
	"errors"
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
	certName, err := resolveReferencedCertificate(ctx, cl, obj, obj.Spec.APISpec.Certificate)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewaySNI %s/%s certificate: %w", obj.Namespace, obj.Name, err)
	}
	sni.Certificate = certName
	return &sni, nil
}

// resolveReferencedCertificate resolves the SNI's certificate reference to the referenced
// certificate's entity name (its spec name, i.e. GetKonnectName). Like resolveEntityName it
// applies no Konnect-ID gate, but it also requires the certificate to target the same
// OnPremAIGateway as the SNI: a certificate targeting another AI Gateway resolves by name, yet
// translateKind never lists it into this gateway's document, so the converter would silently
// drop the SNI from the pushed configuration instead of surfacing the mismatch on the SNI's
// status.
func resolveReferencedCertificate(ctx context.Context, cl client.Client, obj *AIGatewaySNI, ref AIGatewayCertificateRef) (string, error) {
	ns := ref.Namespace
	if ns == "" {
		ns = obj.Namespace
	}
	if ns != obj.Namespace {
		return "", fmt.Errorf("cross-namespace reference to %s/%s is not supported", ns, ref.Name)
	}
	var cert AIGatewayCertificate
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, &cert); err != nil {
		return "", fmt.Errorf("getting referenced AIGatewayCertificate %s/%s: %w", ns, ref.Name, err)
	}
	objKey, ok := onPremAIGatewayRefKey(obj.Namespace, obj.Spec.AIGatewayRef)
	if !ok {
		return "", errors.New("the SNI does not target an OnPremAIGateway")
	}
	certKey, ok := onPremAIGatewayRefKey(cert.Namespace, cert.Spec.AIGatewayRef)
	if !ok || certKey != objKey {
		return "", fmt.Errorf(
			"referenced AIGatewayCertificate %s/%s does not target the SNI's OnPremAIGateway %s",
			ns, ref.Name, objKey,
		)
	}
	return cert.GetKonnectName(), nil
}

// onPremAIGatewayRefKey renders an entity's AIGatewayRef as the ns/name key the
// OnOnPremAIGatewayRef index lists entities under, with the unset Namespace defaulting to the
// entity's own. The second return is false when the ref does not target an OnPremAIGateway at
// all, so comparing two entities' keys is exactly the membership relation translateKind's
// index uses to fill the document.
func onPremAIGatewayRefKey(entityNamespace string, ref AIGatewayRef) (string, bool) {
	if ref.NamespacedRef == nil || !ref.TargetsOnPremAIGateway() {
		return "", false
	}
	ns := entityNamespace
	if ref.NamespacedRef.Namespace != nil && *ref.NamespacedRef.Namespace != "" {
		ns = *ref.NamespacedRef.Namespace
	}
	return ns + "/" + ref.NamespacedRef.Name, true
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
