package v1alpha1

// This file hand-translates AIGatewayModel into github.com/Kong/ai-deck-converter's aigw.Model,
// for on-prem (dbless) config rendering.
// It is deliberately hand-written, and deliberately temporary with the idea to generate
// code below from ai-deck-converter (or other) sources some day:
//
//   - The camelCase-to-snake_case and union-flattening work (marshalSDKOpsPayload,
//     selectedSDKOpsPayload in zz_generated_aigatewaymodel_sdkops.go) is already generated per
//     entity, because the Konnect SDK payload needs the exact same transform. Its output is
//     field-for-field aigw.Model.
//   - The generator lives in the crd-from-oas module (a separate Go module), config at
//     crd-from-oas/config.yaml, regenerated with `make generate.api-from-oas`. The template
//     blocks to extend are in crd-from-oas/pkg/generator/templates.go: sdkOpsRootUnionTemplate
//     (from line 1260, its {{range .Methods}} at line 1775) generates AIGatewayModel; the
//     non-union sdkOpsTemplate (from line 532, {{range .Methods}} at line 874) generates the
//     other nine entity kinds. Both should grow a sibling block, emitted outside
//     {{range .Methods}} (that range enumerates Konnect SDK create/update requests; the on-prem
//     marshaller is one per entity, not one per SDK method), reusing the existing
//     {{- template "sdkOpsRefInjections" $}} define.
//   - The method to generate is MarshalAIGatewayEntity(ctx, cl) ([]byte, error): bytes, not a
//     typed *aigw.Model, so api/ stays free of the ai-deck-converter dependency and callers can
//     stuff the eleven entities' output into a map[string][]json.RawMessage envelope for
//     aigw.Parse. Eleven methods from one template edit.
//   - What survives hand-written even after that: the name-only resolver below (until the
//     generator grows an on-prem resolve mode that skips the Konnect-ID gate described on
//     resolveEntityName), the "managed_by" drop, the model-selector re-nesting, and the
//     semantic balancer lowering (see lowerSemanticBalancer).
//   - Two generator bugs worth fixing while in there, filed separately rather than fixed here:
//     the missing "model"-variant access.acls resolvers (only the "api" variant has
//     RefsAtAIGatewayModelAPIAccessAcls*), and the Konnect-ID gate itself.
//
// ai-deck-converter is not itself generated: aigw/doc.go aliases hand-maintained mirrors of
// ai-gateway-admin-api.yaml. The shared OAS origin is documentary only, so the round-trip test
// in aigatewaymodel_aigw_manual_test.go is the only drift detector between the two sides.
//
// Upstream asks for ai-deck-converter, non-blocking: aigw/doc.go is missing type aliases for
// CACertificate, ModelAccess, and the three model-selector sub-configs
// (ModelBodySelectorConfig, ModelHeaderSelectorConfig, ModelPathSelectorConfig).

import (
	"context"
	"fmt"
	"slices"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWModel converts the AIGatewayModel into ai-deck-converter's aigw.Model, resolving CR
// references to the referenced entities' AI Gateway names.
func (obj *AIGatewayModel) ToAIGWModel(ctx context.Context, cl client.Client) (*aigw.Model, error) {
	spec := &obj.Spec.APISpec
	if spec.AIGatewayModelConfig == nil {
		return nil, fmt.Errorf("AIGatewayModel %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}

	var (
		policies           []AIGatewayPolicyRef
		targets            []AIGatewayTarget
		access             AIGatewayModelAccess
		embeddingsProvider string
	)
	switch spec.Type {
	case AIGatewayModelConfigTypeAPI:
		if spec.API == nil {
			return nil, fmt.Errorf("AIGatewayModel %s/%s: spec.apiSpec.api is required for type api", obj.Namespace, obj.Name)
		}
		policies = spec.API.Policies
		targets = spec.API.Targets
		access = spec.API.Access
		if b := spec.API.Config.Balancer; b != nil && b.Semantic != nil {
			embeddingsProvider = string(b.Semantic.Embeddings.Provider)
		}
	case AIGatewayModelConfigTypeModel:
		if spec.Model == nil {
			return nil, fmt.Errorf("AIGatewayModel %s/%s: spec.apiSpec.model is required for type model", obj.Namespace, obj.Name)
		}
		policies = spec.Model.Policies
		targets = spec.Model.Targets
		access = spec.Model.Access
		if b := spec.Model.Config.Balancer; b != nil && b.Semantic != nil {
			embeddingsProvider = string(b.Semantic.Embeddings.Provider)
		}
	default:
		return nil, fmt.Errorf("AIGatewayModel %s/%s: unsupported spec.apiSpec.type %q", obj.Namespace, obj.Name, spec.Type)
	}

	data, err := spec.marshalAIGWPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayModel %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var model aigw.Model
	if err := yaml.Unmarshal(data, &model); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayModel %s/%s as aigw.Model: %w", obj.Namespace, obj.Name, err)
	}

	if model.Policies, err = resolveEntityNames[AIGatewayPolicy](ctx, cl, obj.Namespace, policyRefs(policies)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayModel %s/%s policies: %w", obj.Namespace, obj.Name, err)
	}

	authStrategies, err := resolveEntityNames[AIGatewayAuthStrategy](ctx, cl, obj.Namespace, authStrategyRefs(access.AuthStrategies))
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayModel %s/%s access.authStrategies: %w", obj.Namespace, obj.Name, err)
	}
	// Resolved current-key refs are re-attached first.
	model.Access.AuthStrategies = append(authStrategies, model.Access.AuthStrategies...)

	if access.Acls != nil {
		var (
			aclRefs []AIGatewayACLRef
			target  *[]string
		)
		switch access.Acls.Type {
		case AIGatewayModelAccessAclsTypeAllow:
			if access.Acls.Allow != nil {
				aclRefs = access.Acls.Allow.Allow
			}
			target = &model.Access.ACLs.Allow
		case AIGatewayModelAccessAclsTypeDeny:
			if access.Acls.Deny != nil {
				aclRefs = access.Acls.Deny.Deny
			}
			target = &model.Access.ACLs.Deny
		// allowWhen and denyWhen carry CEL expressions, not consumer group
		// references, so they go through unresolved.
		case AIGatewayModelAccessAclsTypeAllowWhen:
			if access.Acls.AllowWhen != nil {
				model.Access.ACLs.AllowWhen = slices.Clone(access.Acls.AllowWhen.AllowWhen)
			}
		case AIGatewayModelAccessAclsTypeDenyWhen:
			if access.Acls.DenyWhen != nil {
				model.Access.ACLs.DenyWhen = slices.Clone(access.Acls.DenyWhen.DenyWhen)
			}
		default:
			return nil, fmt.Errorf("AIGatewayModel %s/%s: unsupported access.acls.type %q", obj.Namespace, obj.Name, access.Acls.Type)
		}
		if target != nil {
			if *target, err = resolveEntityNames[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, consumerGroupRefs(aclRefs)); err != nil {
				return nil, fmt.Errorf("resolving AIGatewayModel %s/%s access.acls: %w", obj.Namespace, obj.Name, err)
			}
		}
	}

	if len(targets) != len(model.TargetModels) {
		return nil, fmt.Errorf("AIGatewayModel %s/%s: decoded %d targets, expected %d", obj.Namespace, obj.Name, len(model.TargetModels), len(targets))
	}
	for i, t := range targets {
		name, err := resolveEntityName[AIGatewayModelProvider](ctx, cl, obj.Namespace, t.Provider.Namespace, t.Provider.Name)
		if err != nil {
			return nil, fmt.Errorf("resolving AIGatewayModel %s/%s targets[%d].provider: %w", obj.Namespace, obj.Name, i, err)
		}
		model.TargetModels[i].Provider = name
	}

	// The semantic balancer's embeddings provider is a bare string in the CRD, so it names the
	// referenced AIGatewayModelProvider by metadata.name; the document wants its entity name.
	// The payload strip that keeps it away from the union flattening means it is resolved from
	// the typed spec, not from the decoded model.
	if b := model.Config.Balancer; b != nil && embeddingsProvider != "" {
		if embeddings, ok := b.Fields["embeddings"].(map[string]any); ok {
			name, err := resolveEntityName[AIGatewayModelProvider](ctx, cl, obj.Namespace, "", embeddingsProvider)
			if err != nil {
				return nil, fmt.Errorf("resolving AIGatewayModel %s/%s config.balancer.semantic.embeddings.provider: %w", obj.Namespace, obj.Name, err)
			}
			embeddings["provider"] = name
		}
	}

	return &model, nil
}

// marshalAIGWPayload builds the snake_case JSON bytes for the selected config variant
// (api|model), shaped for direct yaml.Unmarshal into an aigw.Model. It reuses
// marshalSDKOpsPayload/selectedSDKOpsPayload (generated for the Konnect SDK payload, which needs
// the same camelCase->snake_case and union-flattening transform) and edits the intermediate
// payload to strip the CR-reference fields aigw.Model has no room for (they are resolved and
// reattached in ToAIGWModel) and to re-nest the model selector (see renestModelSelector).
func (spec *AIGatewayModelAPISpec) marshalAIGWPayload() ([]byte, error) {
	payload, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	cfg, _ := payload[string(spec.Type)].(map[string]any)
	if cfg == nil {
		return nil, fmt.Errorf("missing %q payload", spec.Type)
	}

	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")

	// These carry {kind,name} CR references; aigw wants plain resolved names. Stripped here,
	// re-attached typed in ToAIGWModel.
	delete(cfg, "policies")
	if a, ok := cfg["access"].(map[string]any); ok {
		delete(a, "auth_strategies")
		delete(a, "acls")
	}
	if ts, ok := cfg["targets"].([]any); ok {
		for _, t := range ts {
			if tm, ok := t.(map[string]any); ok {
				delete(tm, "provider")
			}
		}
	}

	renestModelSelector(cfg)
	lowerSemanticBalancer(cfg)

	data, _, err := spec.selectedSDKOpsPayload(payload)
	return data, err
}

// lowerSemanticBalancer reshapes config.balancer's semantic block in place.
//
// The CRD (like the Konnect API it mirrors) and the ai-deck-converter document
// name two things differently, and the converter passes unknown keys through
// verbatim, so without this the data plane rejects the whole pushed
// configuration with "unknown field"/"required field missing" errors:
//
//   - the vector database strategy discriminator is `type` in the CRD and
//     `strategy` in the ai-deck-converter document. Only that key changes: the
//     union flattening that runs after this edit lifts the selected member's
//     settings (dimensions, host, ssl, ...) next to it either way;
//   - the embeddings model is flat in the CRD (`name`, `config`) and nested
//     under `model` in the ai-deck-converter document, which is also where the
//     converter resolves the embeddings' provider auth from;
//   - the CRD's `allow_auth_override` sits at the embeddings level, while the
//     plugin (and so the converter) carries it as `auth.allow_override`.
//
// `provider` is stripped here and re-attached resolved by ToAIGWModel, like
// targets' provider: it is a bare string in the CRD (not a {kind,name} object)
// that names the referenced AIGatewayModelProvider by metadata.name, and left in
// place it would be read by selectedSDKOpsPayload's union flattening as a
// discriminator: a provider CR named "model" (or "auth") points at the sibling
// map created below, which the flattening would delete and hoist.
//
// See https://github.com/Kong/kong-operator/issues/6231.
func lowerSemanticBalancer(cfg map[string]any) {
	modelConfig, ok := cfg["config"].(map[string]any)
	if !ok {
		return
	}
	balancer, ok := modelConfig["balancer"].(map[string]any)
	if !ok {
		return
	}
	// The algorithm's settings still sit under a member named by the algorithm
	// itself; selectedSDKOpsPayload flattens that member into the balancer block
	// afterwards, which keeps the two edits below independent of each other.
	algorithm, _ := balancer["algorithm"].(string)
	if algorithm != "semantic" {
		return
	}
	semantic, ok := balancer[algorithm].(map[string]any)
	if !ok {
		return
	}
	if vectordb, ok := semantic["vectordb"].(map[string]any); ok {
		if strategy, ok := vectordb["type"]; ok {
			delete(vectordb, "type")
			vectordb["strategy"] = strategy
		}
	}
	embeddings, ok := semantic["embeddings"].(map[string]any)
	if !ok {
		return
	}
	delete(embeddings, "provider")
	model := map[string]any{}
	for _, field := range []string{"name", "config"} {
		if v, ok := embeddings[field]; ok {
			model[field] = v
			delete(embeddings, field)
		}
	}
	if len(model) > 0 {
		embeddings["model"] = model
	}
	if allow, ok := embeddings["allow_auth_override"]; ok {
		delete(embeddings, "allow_auth_override")
		auth, _ := embeddings["auth"].(map[string]any)
		if auth == nil {
			auth = map[string]any{}
			embeddings["auth"] = auth
		}
		auth["allow_override"] = allow
	}
}

// renestModelSelector fixes up config.route.model in place.
//
// The CRD's AIGatewayModelSelectorConfig (like the Konnect API it mirrors) is flat: bodyParam,
// headerParam, pathParam, values. aigw.ModelSelectorConfig nests the first three under
// body/header/path sub-objects. Without this, config.route.model is silently dropped on decode
// (aigw/doc.go doesn't even alias the sub-config types, so this can't be done as a typed
// assignment from outside the module).
func renestModelSelector(cfg map[string]any) {
	route, ok := cfg["config"].(map[string]any)
	if !ok {
		return
	}
	route, ok = route["route"].(map[string]any)
	if !ok {
		return
	}
	selector, ok := route["model"].(map[string]any)
	if !ok {
		return
	}
	for param, group := range map[string]string{
		"body_param":   "body",
		"header_param": "header",
		"path_param":   "path",
	} {
		if v, ok := selector[param]; ok {
			delete(selector, param)
			selector[group] = map[string]any{param: v}
		}
	}
}

// policyRefs, authStrategyRefs and consumerGroupRefs project the CRD's per-kind reference types
// (AIGatewayPolicyRef, AIGatewayAuthStrategyRef, AIGatewayACLRef) onto the common shape
// resolveEntityNames needs. Each ref type is a distinct generated struct with no shared
// interface, so this is unavoidably one small loop per type.

func policyRefs(refs []AIGatewayPolicyRef) []namespacedRef {
	out := make([]namespacedRef, len(refs))
	for i, r := range refs {
		out[i] = namespacedRef{Name: r.Name, Namespace: r.Namespace}
	}
	return out
}

func authStrategyRefs(refs []AIGatewayAuthStrategyRef) []namespacedRef {
	out := make([]namespacedRef, len(refs))
	for i, r := range refs {
		out[i] = namespacedRef{Name: r.Name, Namespace: r.Namespace}
	}
	return out
}

func consumerGroupRefs(refs []AIGatewayACLRef) []namespacedRef {
	out := make([]namespacedRef, len(refs))
	for i, r := range refs {
		out[i] = namespacedRef{Name: r.Name, Namespace: r.Namespace}
	}
	return out
}

// namespacedRef is the common shape of the AI Gateway entity reference types.
type namespacedRef struct {
	Name      string
	Namespace string
}

// resolveEntityNames resolves sibling AI Gateway entity references to the referenced objects'
// entity names (their spec `name`, i.e. GetKonnectName).
func resolveEntityNames[T any, PT interface {
	*T
	client.Object
	GetKonnectName() string
}](ctx context.Context, cl client.Client, defaultNamespace string, refs []namespacedRef) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		name, err := resolveEntityName[T, PT](ctx, cl, defaultNamespace, ref.Namespace, ref.Name)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

// resolveEntityName resolves a single sibling AI Gateway entity reference to the referenced
// object's entity name (its spec `name`, i.e. GetKonnectName).
//
// Unlike the generated resolve* helpers in zz_generated_aigatewaymodel_sdkops.go (e.g.
// resolveAIGatewayModelModelTargetsProvider), this does not require the referenced object to
// carry a Konnect ID: on-prem entities are never programmed in Konnect. The Konnect-ID gate is
// the only thing making those generated helpers Konnect-specific; see this file's package
// comment for relaxing it in the generator instead of hand-writing this resolver.
func resolveEntityName[T any, PT interface {
	*T
	client.Object
	GetKonnectName() string
}](ctx context.Context, cl client.Client, defaultNamespace, refNamespace, refName string) (string, error) {
	ns := refNamespace
	if ns == "" {
		ns = defaultNamespace
	}
	if ns != defaultNamespace {
		return "", fmt.Errorf("cross-namespace reference to %s/%s is not supported", ns, refName)
	}
	var obj T
	p := PT(&obj)
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: refName}, p); err != nil {
		return "", fmt.Errorf("getting referenced %T %s/%s: %w", obj, ns, refName, err)
	}
	return p.GetKonnectName(), nil
}
