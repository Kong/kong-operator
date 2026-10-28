package v1alpha1

// This file hand-translates AIGatewayConsumer into ai-deck-converter's aigw.Consumer,
// for on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment
// for the shared background and the plan to generate this some day.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CredentialTranslation is the outcome of translating one of the credentials a consumer
// embeds: Err is nil for the credential included in the returned aigw.Consumer, and the
// translation error for the one left out of it.
//
// It is a translation result, never a field of an API type, so it needs no deepcopy.
//
// +kubebuilder:object:generate=false
type CredentialTranslation struct {
	// Obj is the credential the outcome belongs to.
	Obj *AIGatewayConsumerCredential
	// Err is the translation error, nil when the credential was embedded in the consumer.
	Err error
}

// ToAIGWConsumer converts the AIGatewayConsumer into ai-deck-converter's aigw.Consumer,
// resolving spec.apiSpec.policies and spec.consumerGroups references to the referenced
// entities' names and embedding the credentials referencing it (each with its secretRef
// apiKey resolved).
//
// The credentials are translated one by one and their outcome is returned alongside the
// consumer (see CredentialTranslation), so that the caller can report a broken credential
// on the credential itself: a credential whose translation fails is left out of the
// consumer, and the consumer still carries its healthy credentials.
//
// On error the credential outcomes gathered so far are returned too (a broken credential
// keeps its own Err; the others carry none, since nothing of theirs is at fault): the caller
// reports them against the credential, which otherwise holds a stale Programmed condition -
// its consumer's failure keeps it out of the pushed configuration, and the status reporter
// only notifies the entities of the current sync.
//
// credIndexField is the field name of the AIGatewayConsumerCredential ->
// AIGatewayConsumer index (internal/utils/index's
// IndexFieldAIGatewayConsumerCredentialOnAIGatewayConsumerRef). It is passed in by the
// caller (the on-prem translator) because this api package cannot import that package
// without a cycle.
func (obj *AIGatewayConsumer) ToAIGWConsumer(
	ctx context.Context,
	cl client.Client,
	credIndexField string,
) (*aigw.Consumer, []CredentialTranslation, error) {
	data, err := obj.Spec.APISpec.marshalAIGWConsumerPayload()
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling AIGatewayConsumer %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var consumer aigw.Consumer
	if err := yaml.Unmarshal(data, &consumer); err != nil {
		return nil, nil, fmt.Errorf("decoding AIGatewayConsumer %s/%s as aigw.Consumer: %w", obj.Namespace, obj.Name, err)
	}

	// Translated before the references below, so that their failure returns the credentials
	// already gathered (see the doc comment).
	credentials, credentialTranslations, err := obj.aigwCredentials(ctx, cl, credIndexField)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving AIGatewayConsumer %s/%s credentials: %w", obj.Namespace, obj.Name, err)
	}
	consumer.Credentials = credentials

	if consumer.Policies, err = resolveEntityNames[AIGatewayPolicy](ctx, cl, obj.Namespace, policyRefs(obj.Spec.APISpec.Policies)); err != nil {
		return nil, credentialTranslations, fmt.Errorf("resolving AIGatewayConsumer %s/%s policies: %w", obj.Namespace, obj.Name, err)
	}
	if consumer.ConsumerGroups, err = resolveEntityNames[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, consumerGroupNamespacedRefs(obj.Spec.ConsumerGroups)); err != nil {
		return nil, credentialTranslations, fmt.Errorf("resolving AIGatewayConsumer %s/%s consumer groups: %w", obj.Namespace, obj.Name, err)
	}
	return &consumer, credentialTranslations, nil
}

// marshalAIGWConsumerPayload builds the aigw.Consumer-shaped payload bytes. Shared by
// ToAIGWConsumer and its strict round-trip test, so the test decodes the production
// pipeline's output, not a copy of it.
func (spec *AIGatewayConsumerAPISpec) marshalAIGWConsumerPayload() ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayConsumer SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	// These carry {kind,name} CR references; aigw wants plain resolved names. Stripped here,
	// re-attached resolved in ToAIGWConsumer.
	delete(cfg, "policies")
	return json.Marshal(cfg)
}

// consumerGroupNamespacedRefs projects the AIGatewayConsumer's consumer group references onto
// the common shape resolveEntityNames needs (same-namespace, name-only refs).
func consumerGroupNamespacedRefs(refs []AIGatewayConsumerGroupRef) []namespacedRef {
	out := make([]namespacedRef, len(refs))
	for i, r := range refs {
		out[i] = namespacedRef{Name: r.Name}
	}
	return out
}

// aigwCredentials translates the AIGatewayConsumerCredentials referencing this consumer into
// aigw.Credentials, sorted by k8s name so the rendered document (and the payload hash derived
// from it) doesn't flap across List calls that return in a different order.
//
// The credentials are listed via the credIndexField index (see ToAIGWConsumer), so only the
// ones referencing this consumer are read from the cache. The index value is
// "<refNamespace>/<refName>", with refNamespace defaulting to the credential's namespace —
// mirroring resolveEntityNames's same-namespace rule for entity references.
//
// A credential that fails to translate is left out of the returned credentials and reported
// through its CredentialTranslation: the remaining credentials still render, mirroring KIC's
// continue-on-error translation. Only failing to list the credentials is an error here: it
// would silently drop all of the consumer's credentials.
func (obj *AIGatewayConsumer) aigwCredentials(
	ctx context.Context,
	cl client.Client,
	credIndexField string,
) ([]aigw.Credential, []CredentialTranslation, error) {
	var list AIGatewayConsumerCredentialList
	if err := cl.List(ctx, &list,
		client.InNamespace(obj.Namespace),
		client.MatchingFields{credIndexField: obj.Namespace + "/" + obj.Name},
	); err != nil {
		return nil, nil, fmt.Errorf("listing AIGatewayConsumerCredentials: %w", err)
	}

	slices.SortFunc(list.Items, func(a, b AIGatewayConsumerCredential) int {
		return strings.Compare(a.Name, b.Name)
	})

	var (
		creds        []aigw.Credential
		translations = make([]CredentialTranslation, 0, len(list.Items))
	)
	for i := range list.Items {
		cred := &list.Items[i]
		translated, err := cred.toAIGWCredential(ctx, cl)
		if err != nil {
			translations = append(translations, CredentialTranslation{Obj: cred, Err: err})
			continue
		}
		creds = append(creds, *translated)
		translations = append(translations, CredentialTranslation{Obj: cred})
	}
	return creds, translations, nil
}

// toAIGWCredential converts a single AIGatewayConsumerCredential into ai-deck-converter's
// aigw.Credential, resolving its secretRef apiKey. Cross-namespace secretRefs are rejected
// per the on-prem rule (the generated sdkOpsAPISpec alone would resolve them regardless of
// namespace).
func (obj *AIGatewayConsumerCredential) toAIGWCredential(ctx context.Context, cl client.Client) (*aigw.Credential, error) {
	if err := rejectCrossNamespaceSecretRefs(obj); err != nil {
		return nil, err
	}

	resolvedSpec, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		// An existing-but-unlabelled Secret reads as not found through the operator's
		// filtered cache: annotate the not-found error with the label requirement.
		return nil, noteSecretLabelRequirement(err)
	}
	data, err := marshalAIGWConsumerCredentialPayload(resolvedSpec)
	if err != nil {
		return nil, err
	}

	var cred aigw.Credential
	if err := yaml.Unmarshal(data, &cred); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayConsumerCredential %s/%s as aigw.Credential: %w", obj.Namespace, obj.Name, err)
	}
	return &cred, nil
}

// marshalAIGWConsumerCredentialPayload builds the aigw.Credential-shaped payload bytes from an
// already secret-resolved AIGatewayConsumerCredentialAPISpec.
func marshalAIGWConsumerCredentialPayload(spec *AIGatewayConsumerCredentialAPISpec) ([]byte, error) {
	data, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayConsumerCredential SDK payload: %w", err)
	}
	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")
	return json.Marshal(cfg)
}
