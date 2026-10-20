/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package translator

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/Kong/ai-deck-converter/convert"
	yaml "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// defaultMinRuntimeVersion is the minimum AI Gateway runtime version declared for
// rendered configurations when the caller does not specify one: the oldest release
// that must run the pushed output. Pinned to the converter README's example value;
// TODO: derive it from the actual data plane version tag set on AIGatewayDataPlane
// TODO: https://github.com/Kong/kong-operator/issues/6120
const defaultMinRuntimeVersion = "2.1"

// EntityStatus is the per-entity outcome of the document translation: Err is
// nil for entities that were translated and included in the document, and the
// conversion error for the ones that were not (they are excluded from the
// document so that a single broken entity does not block the rest).
type EntityStatus struct {
	Obj client.Object
	Err error
}

// translateKind returns a func that lists every entity of one kind pointing at the
// given OnPremAIGateway (via the kind's generated OnOnPremAIGatewayRef index), sorts
// the items by namespace/name so the rendered document (and the payload hash derived
// from it) doesn't flap across List calls that return in a different order, converts
// each item with convert and appends it to dest.
//
// Only TList is named at the call site: T is inferred from the list's GetItems and
// TListPtr from the *TList core type (same pattern as
// enqueueObjectForKonnectGatewayControlPlane in controller/konnect/watch.go).
//
// A per-entity conversion failure does not abort the whole translation: the failing entity is
// excluded from the document and reported in the returned statuses, so that the remaining
// entities still render and get pushed (mirroring KIC's continue-on-error translation).
// A failure to list the entities of a kind is a different beast: the whole kind's contribution
// is missing from the document, so it aborts the translation (and no status is reported).
//
// T's method set lives on the pointer, hence the metav1.Object assertions below — infallible
// for every aiconfiguration list item.
func translateKind[
	TList interface {
		GetItems() []T
	},
	TListPtr interface {
		*TList
		client.ObjectList
		GetItems() []T
	},
	T any,
	AIGWEntity any,
](
	cl client.Client,
	gw types.NamespacedName,
	indexField string,
	convert func(*T, context.Context, client.Client) (*AIGWEntity, error),
	dest *[]AIGWEntity,
) func(context.Context) ([]EntityStatus, error) {
	return func(ctx context.Context) ([]EntityStatus, error) {
		var (
			l    TList
			lPtr TListPtr = &l
		)

		if err := cl.List(ctx, lPtr, client.MatchingFields{indexField: gw.String()}); err != nil {
			return nil, fmt.Errorf("listing %T for %s: %w", lPtr, gw, err)
		}
		items := lPtr.GetItems()
		slices.SortFunc(items, func(a, b T) int {
			aObj, bObj := any(&a).(metav1.Object), any(&b).(metav1.Object)
			return cmp.Or(
				cmp.Compare(aObj.GetNamespace(), bObj.GetNamespace()),
				cmp.Compare(aObj.GetName(), bObj.GetName()),
			)
		})
		statuses := make([]EntityStatus, 0, len(items))
		for i := range items {
			obj := any(&items[i]).(client.Object)
			// The OnOnPremAIGatewayRef index matches entities regardless of their namespace, but
			// the on-prem path is same-namespace only (consistent with resolveEntityName's
			// entity references and rejectCrossNamespaceSecretRefs): an entity in another
			// namespace is listed here only to be rejected with a clear per-entity error,
			// instead of failing later on its Secrets being invisible to the gateway-namespace
			// scoped cache.
			// TODO: support cross-namespace references, tracked in
			// https://github.com/Kong/kong-operator/issues/5957.
			if obj.GetNamespace() != gw.Namespace {
				statuses = append(statuses, EntityStatus{
					Obj: obj,
					Err: fmt.Errorf(
						"cross-namespace reference to OnPremAIGateway %s is not supported on-prem: the entity must live in the gateway's namespace %s",
						gw, gw.Namespace,
					),
				})
				continue
			}

			aigwEntity, err := convert(&items[i], ctx, cl)
			if err != nil {
				statuses = append(statuses, EntityStatus{
					Obj: obj,
					Err: fmt.Errorf("converting %T %s: %w", obj, client.ObjectKeyFromObject(obj), err),
				})
				continue
			}
			*dest = append(*dest, *aigwEntity)
			statuses = append(statuses, EntityStatus{Obj: obj})
		}
		return statuses, nil
	}
}

// checkTranslatedRefs fails the referencing entity when any referenced name is missing from
// the set of entities of that kind that translated successfully: resolveEntityNames only
// checks the referenced entity's existence, so without this a referenced entity excluded for
// its own failed translation would let the referencing entity report success while the
// converter drops it from the pushed payload (or silently omits the reference).
func checkTranslatedRefs(translated map[string]struct{}, kind string, names []string) error {
	for _, name := range names {
		if _, ok := translated[name]; !ok {
			return fmt.Errorf(
				"referenced %s %q was excluded from the document: its own translation failed",
				kind, name,
			)
		}
	}
	return nil
}

// checkTranslatedACLs checks both ACL name lists: every entry comes from a resolved
// consumer-group CR reference, so each must point at a translated AIGatewayConsumerGroup.
func checkTranslatedACLs(translated map[string]struct{}, acls aigw.ACLs) error {
	return cmp.Or(
		checkTranslatedRefs(translated, "AIGatewayConsumerGroup", acls.Allow),
		checkTranslatedRefs(translated, "AIGatewayConsumerGroup", acls.Deny),
	)
}

// BuildDocument assembles the aigw.Document for the given OnPremAIGateway, translating every
// aiconfiguration entity kind pointing at it. Alongside the document it returns the per-entity
// translation status (see EntityStatus), so that the caller can report each entity's outcome on
// the entity's status.
//
// Entity kinds join here as they gain their own ToAIGW* conversion; until then the rendered
// Document (and the dbless payload built from it) has dangling references, which
// RenderDBLessYAML reports as converter issues instead of errors.
//
// The OnOnPremAIGatewayRef index extractor only matches entities whose aiGatewayRef resolves
// to an OnPremAIGateway, so a KonnectAIGateway sharing namespace/name with this
// OnPremAIGateway never collides.
//
// NOTE: This will either stay here or be moved to a separate package where translation
// (building the document) will happen asynchronously as it's done for ingress-controller.
func BuildDocument(
	ctx context.Context,
	cl client.Client,
	gw types.NamespacedName,
) (*aigw.Document, []EntityStatus, error) {
	doc := &aigw.Document{}
	var statuses []EntityStatus
	// Entity names of the entities that translated successfully, checked when translating
	// the entities referencing them: the existence-only resolvers (resolveEntityNames) only
	// check the referenced entity's existence and same-gateway membership, so without these
	// a referenced entity that failed its own translation (e.g. a missing or unlabeled
	// secretRef Secret) would leave its referent reporting success while the converter drops
	// the referent from the pushed payload. Cross-kind dependencies are ordered first in the
	// translate list below so the referent's check sees the complete set; the one same-kind
	// dependency (MCPServer sources) is validated by the fixpoint pass after the loop.
	translatedCertificates := make(map[string]struct{})
	translatedModelProviders := make(map[string]struct{})
	translatedPolicies := make(map[string]struct{})
	translatedConsumerGroups := make(map[string]struct{})
	translatedAuthStrategies := make(map[string]struct{})
	translatedMCPServers := make(map[string]struct{})
	// translatedMCPCRByName maps a translated MCPServer's entity name to its original CR, so
	// the same-kind sources post-pass can attribute a dropped listener's failure to the CR
	// whose EntityStatus already reported success (the maps store the same pointers
	// translateKind put into the statuses).
	translatedMCPCRByName := make(map[string]client.Object)

	// CA cert PEMs already claimed by a translated AIGatewayCACertificate, mapped to the
	// claiming CR's name: ai-deck-converter derives the rendered entity ID from the cert
	// content (convert/dbless.go's stableUUID("ca_certificate:"+cert)), so two CRs sharing
	// one PEM would render two entries with the same ID and fail the whole push. The first
	// CR in namespace/name order wins; the duplicate is reported per-entity and excluded
	// from the document.
	translatedCACertCRs := make(map[string]string)

	// Vault prefixes already claimed by a translated AIGatewayVault, mapped to the claiming
	// CR's name: ai-deck-converter derives the rendered entity ID from the vault prefix
	// (internal/kong/to_dbless.go's stableUUID("vault:"+prefix)), so two CRs sharing a prefix
	// would render two entries with the same ID and fail the whole push. The first CR in
	// namespace/name order wins; the duplicate is reported per-entity and excluded from the
	// document.
	translatedVaultPrefixes := make(map[string]string)

	// The list is ordered so every kind translates after all the kinds it references:
	// checkTranslatedRefs inside the converting wrappers relies on the referenced kinds'
	// translated-name sets being complete by then. The one same-kind dependency
	// (MCPServer -> MCPServer sources) can't be ordered this way, hence the post-pass below.
	for _, translate := range []func(context.Context) ([]EntityStatus, error){
		translateKind[aiconfigurationv1alpha1.AIGatewayModelProviderList](
			cl, gw, index.IndexFieldAIGatewayModelProviderOnOnPremAIGatewayRef,
			func(p *aiconfigurationv1alpha1.AIGatewayModelProvider, ctx context.Context, cl client.Client) (*aigw.Provider, error) {
				provider, err := p.ToAIGWProvider(ctx, cl)
				if err != nil {
					return nil, err
				}
				translatedModelProviders[p.GetKonnectName()] = struct{}{}
				return provider, nil
			},
			&doc.ModelProviders),
		translateKind[aiconfigurationv1alpha1.AIGatewayPolicyList](
			cl, gw, index.IndexFieldAIGatewayPolicyOnOnPremAIGatewayRef,
			func(p *aiconfigurationv1alpha1.AIGatewayPolicy, ctx context.Context, cl client.Client) (*aigw.Policy, error) {
				policy, err := p.ToAIGWPolicy(ctx, cl)
				if err != nil {
					return nil, err
				}
				translatedPolicies[p.GetKonnectName()] = struct{}{}
				return policy, nil
			},
			&doc.Policies),
		translateKind[aiconfigurationv1alpha1.AIGatewayCustomPolicyList](
			cl, gw, index.IndexFieldAIGatewayCustomPolicyOnOnPremAIGatewayRef,
			(*aiconfigurationv1alpha1.AIGatewayCustomPolicy).ToAIGWCustomPolicy, &doc.CustomPolicies),
		translateKind[aiconfigurationv1alpha1.AIGatewayConsumerGroupList](
			cl, gw, index.IndexFieldAIGatewayConsumerGroupOnOnPremAIGatewayRef,
			func(g *aiconfigurationv1alpha1.AIGatewayConsumerGroup, ctx context.Context, cl client.Client) (*aigw.ConsumerGroup, error) {
				group, err := g.ToAIGWConsumerGroup(ctx, cl)
				if err != nil {
					return nil, err
				}
				if err := checkTranslatedRefs(translatedPolicies, "AIGatewayPolicy", group.Policies); err != nil {
					return nil, err
				}
				translatedConsumerGroups[g.GetKonnectName()] = struct{}{}
				return group, nil
			},
			&doc.ConsumerGroups),
		translateKind[aiconfigurationv1alpha1.AIGatewayAuthStrategyList](
			cl, gw, index.IndexFieldAIGatewayAuthStrategyOnOnPremAIGatewayRef,
			func(a *aiconfigurationv1alpha1.AIGatewayAuthStrategy, ctx context.Context, cl client.Client) (*aigw.AuthStrategy, error) {
				authStrategy, err := a.ToAIGWAuthStrategy(ctx, cl)
				if err != nil {
					return nil, err
				}
				translatedAuthStrategies[a.GetKonnectName()] = struct{}{}
				return authStrategy, nil
			},
			&doc.AuthStrategies),
		translateKind[aiconfigurationv1alpha1.AIGatewayModelList](
			cl, gw, index.IndexFieldAIGatewayModelOnOnPremAIGatewayRef,
			func(m *aiconfigurationv1alpha1.AIGatewayModel, ctx context.Context, cl client.Client) (*aigw.Model, error) {
				model, err := m.ToAIGWModel(ctx, cl)
				if err != nil {
					return nil, err
				}
				if err := cmp.Or(
					checkTranslatedRefs(translatedPolicies, "AIGatewayPolicy", model.Policies),
					checkTranslatedRefs(translatedAuthStrategies, "AIGatewayAuthStrategy", model.Access.AuthStrategies),
					checkTranslatedACLs(translatedConsumerGroups, model.Access.ACLs),
				); err != nil {
					return nil, err
				}
				for i := range model.TargetModels {
					if err := checkTranslatedRefs(
						translatedModelProviders, "AIGatewayModelProvider",
						[]string{model.TargetModels[i].Provider},
					); err != nil {
						return nil, err
					}
				}
				return model, nil
			},
			&doc.Models),
		translateKind[aiconfigurationv1alpha1.AIGatewayConsumerList](
			cl, gw, index.IndexFieldAIGatewayConsumerOnOnPremAIGatewayRef,
			// The consumer's credential lookup uses this package's index for
			// AIGatewayConsumerCredential -> AIGatewayConsumer: the field name is passed in
			// because the api package cannot import internal/utils/index without a cycle.
			func(c *aiconfigurationv1alpha1.AIGatewayConsumer, ctx context.Context, cl client.Client) (*aigw.Consumer, error) {
				consumer, err := c.ToAIGWConsumer(ctx, cl, index.IndexFieldAIGatewayConsumerCredentialOnAIGatewayConsumerRef)
				if err != nil {
					return nil, err
				}
				if err := cmp.Or(
					checkTranslatedRefs(translatedPolicies, "AIGatewayPolicy", consumer.Policies),
					checkTranslatedRefs(translatedConsumerGroups, "AIGatewayConsumerGroup", consumer.ConsumerGroups),
				); err != nil {
					return nil, err
				}
				return consumer, nil
			},
			&doc.Consumers),
		translateKind[aiconfigurationv1alpha1.AIGatewayAgentList](
			cl, gw, index.IndexFieldAIGatewayAgentOnOnPremAIGatewayRef,
			func(a *aiconfigurationv1alpha1.AIGatewayAgent, ctx context.Context, cl client.Client) (*aigw.Agent, error) {
				agent, err := a.ToAIGWAgent(ctx, cl)
				if err != nil {
					return nil, err
				}
				if err := cmp.Or(
					checkTranslatedRefs(translatedPolicies, "AIGatewayPolicy", agent.Policies),
					checkTranslatedACLs(translatedConsumerGroups, agent.Access.ACLs),
					checkTranslatedRefs(translatedAuthStrategies, "AIGatewayAuthStrategy", agent.Access.AuthStrategies),
				); err != nil {
					return nil, err
				}
				return agent, nil
			},
			&doc.Agents),
		translateKind[aiconfigurationv1alpha1.AIGatewayMCPServerList](
			cl, gw, index.IndexFieldAIGatewayMCPServerOnOnPremAIGatewayRef,
			func(s *aiconfigurationv1alpha1.AIGatewayMCPServer, ctx context.Context, cl client.Client) (*aigw.MCPServer, error) {
				mcpServer, err := s.ToAIGWMCPServer(ctx, cl)
				if err != nil {
					return nil, err
				}
				// Config.Sources is deliberately not checked here: sources reference the same
				// kind, so a source may be translated after this entity (see the post-pass).
				if err := cmp.Or(
					checkTranslatedRefs(translatedPolicies, "AIGatewayPolicy", mcpServer.Policies),
					checkTranslatedACLs(translatedConsumerGroups, mcpServer.Access.ACLs),
					checkTranslatedACLs(translatedConsumerGroups, mcpServer.Access.DefaultToolACLs),
					checkTranslatedRefs(translatedAuthStrategies, "AIGatewayAuthStrategy", mcpServer.Access.AuthStrategies),
				); err != nil {
					return nil, err
				}
				for i := range mcpServer.Tools {
					if err := checkTranslatedACLs(
						translatedConsumerGroups, mcpServer.Tools[i].Access.ACLs,
					); err != nil {
						return nil, err
					}
				}
				name := s.GetKonnectName()
				if _, dup := translatedMCPServers[name]; dup {
					return nil, fmt.Errorf(
						"duplicate AIGatewayMCPServer entity name %q: entity names must be unique across AIGatewayMCPServer CRs",
						name,
					)
				}
				translatedMCPServers[name] = struct{}{}
				translatedMCPCRByName[name] = s
				return mcpServer, nil
			},
			&doc.MCPServers),
		translateKind[aiconfigurationv1alpha1.AIGatewayVaultList](
			cl, gw, index.IndexFieldAIGatewayVaultOnOnPremAIGatewayRef,
			func(v *aiconfigurationv1alpha1.AIGatewayVault, ctx context.Context, cl client.Client) (*aigw.Vault, error) {
				vault, err := v.ToAIGWVault(ctx, cl)
				if err != nil {
					return nil, err
				}
				if first, ok := translatedVaultPrefixes[vault.Name]; ok {
					return nil, fmt.Errorf(
						"duplicate vault prefix %q: AIGatewayVault %q already uses it, and the rendered entity ID is derived from the vault prefix",
						vault.Name, first,
					)
				}
				translatedVaultPrefixes[vault.Name] = v.Name
				return vault, nil
			},
			&doc.Vaults),
		translateKind[aiconfigurationv1alpha1.AIGatewayCACertificateList](
			cl, gw, index.IndexFieldAIGatewayCACertificateOnOnPremAIGatewayRef,
			func(c *aiconfigurationv1alpha1.AIGatewayCACertificate, ctx context.Context, cl client.Client) (*aigw.CACertificate, error) {
				caCert, err := c.ToAIGWCACertificate(ctx, cl)
				if err != nil {
					return nil, err
				}
				if first, ok := translatedCACertCRs[caCert.Cert]; ok {
					return nil, fmt.Errorf(
						"duplicate CA certificate content: AIGatewayCACertificate %q already carries this cert, and the rendered entity ID is derived from the cert content",
						first,
					)
				}
				translatedCACertCRs[caCert.Cert] = c.Name
				return caCert, nil
			},
			&doc.CACertificates),
		translateKind[aiconfigurationv1alpha1.AIGatewayCertificateList](
			cl, gw, index.IndexFieldAIGatewayCertificateOnOnPremAIGatewayRef,
			func(c *aiconfigurationv1alpha1.AIGatewayCertificate, ctx context.Context, cl client.Client) (*aigw.Certificate, error) {
				cert, err := c.ToAIGWCertificate(ctx, cl)
				if err != nil {
					return nil, err
				}
				translatedCertificates[c.GetKonnectName()] = struct{}{}
				return cert, nil
			},
			&doc.Certificates),
		translateKind[aiconfigurationv1alpha1.AIGatewaySNIList](
			cl, gw, index.IndexFieldAIGatewaySNIOnOnPremAIGatewayRef,
			func(s *aiconfigurationv1alpha1.AIGatewaySNI, ctx context.Context, cl client.Client) (*aigw.SNI, error) {
				sni, err := s.ToAIGWSNI(ctx, cl)
				if err != nil {
					return nil, err
				}
				if _, ok := translatedCertificates[sni.Certificate]; !ok {
					return nil, fmt.Errorf(
						"referenced AIGatewayCertificate %q was excluded from the document: its own translation failed",
						sni.Certificate,
					)
				}
				return sni, nil
			},
			&doc.SNIs),
	} {
		s, err := translate(ctx)
		if err != nil {
			return nil, nil, err
		}
		statuses = append(statuses, s...)
	}

	// MCPServer sources reference the same kind, so a translate-time check would wrongly
	// reject a listener whose source is sorted later in the kind (its own translation may
	// not have happened yet): validate after all MCP servers translated, dropping listeners
	// whose sources point outside the translated set. Dropping one listener can orphan
	// another listener referencing it, hence the fixpoint. A dropped listener's status
	// reported success during translation, so it is patched in place via pointer identity
	// with the CR stored in translatedMCPCRByName.
	// ponytail: the fixpoint cascade is scoped to MCPServer sources; if another kind ever
	// gains same-kind references, generalize this pass instead of copying it.
	for dropped := true; dropped; {
		dropped = false
		kept := make([]aigw.MCPServer, 0, len(doc.MCPServers))
		for _, m := range doc.MCPServers {
			missing := ""
			for _, src := range m.Config.Sources {
				if _, ok := translatedMCPServers[src]; !ok {
					missing = src
					break
				}
			}
			if missing == "" {
				kept = append(kept, m)
				continue
			}
			dropped = true
			delete(translatedMCPServers, m.Name)
			cr, ok := translatedMCPCRByName[m.Name]
			if !ok {
				continue
			}
			for i := range statuses {
				if statuses[i].Obj == cr {
					statuses[i].Err = fmt.Errorf(
						"referenced AIGatewayMCPServer %q was excluded from the document: its own translation failed, or it references an excluded server",
						missing,
					)
				}
			}
		}
		doc.MCPServers = kept
	}

	return doc, statuses, nil
}

// stubUpstreamName is the name of the inert upstream that is pushed as the only
// entity when the configuration is otherwise empty. It mirrors the ingress
// controller's deckgen.StubUpstreamName, declared again here because that one
// lives under ingress-controller/internal and is not importable.
const stubUpstreamName = "kong"

// RenderDBLessYAML converts the translated AI Gateway document into the flattened
// db-less YAML payload understood by Kong data planes. Alongside the payload it
// returns the typed conversion issues reported by the converter: a Warning issue
// means the converter worked around a problem (the output is valid but may not
// fully match the intent), while an Error issue means the offending entity was
// left out of the output entirely - the caller is expected to attribute those
// back to the entities' CRs (see the aigateway package's attributeConversionIssues).
//
// The payload is never empty: see ensureNonEmptyConfig.
func RenderDBLessYAML(doc *aigw.Document, opts convert.Options) ([]byte, []convert.ConversionIssue, error) {
	if opts.MinRuntimeVersion == "" {
		// Pinned minimum; see defaultMinRuntimeVersion.
		opts.MinRuntimeVersion = defaultMinRuntimeVersion
	}
	// The converter's public entry point (convert.Convert) only accepts YAML bytes,
	// so the document built by BuildDocument goes through a marshal/reparse
	// round-trip: aigw.Document is the converter's file format, so it round-trips
	// losslessly.
	src, err := yaml.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling AI Gateway document: %w", err)
	}
	result, err := convert.Convert(src, opts)
	if err != nil {
		// Pass any issues the converter reported before failing through to the
		// caller, so they still reach the gateway status.
		if result != nil {
			return nil, result.Issues, err
		}
		return nil, nil, err
	}
	payload, err := result.Output.ToDBLess().ToYAML()
	if err != nil {
		return nil, result.Issues, err
	}
	payload, err = ensureNonEmptyConfig(payload)
	return payload, result.Issues, err
}

// ensureNonEmptyConfig appends the stub upstream when the rendered payload holds no
// entities, and returns the payload unchanged otherwise.
//
// A Kong data plane does not report ready (GET /status/ready never returns 200) while
// its applied configuration is empty, so a gateway with no configuration entities yet
// would never become ready. This is the same workaround the ingress controller applies
// in db-less mode.
//
// Emptiness is decided on the rendered payload rather than on the document, so a
// document whose entities were all dropped by the converter is covered too. Only
// metadata keys are prefixed with an underscore (_format_version, _transform), so any
// other top-level key means the payload carries entities.
func ensureNonEmptyConfig(payload []byte) ([]byte, error) {
	var content map[string]any
	if err := yaml.Unmarshal(payload, &content); err != nil {
		return nil, fmt.Errorf("parsing rendered dbless configuration: %w", err)
	}
	for key := range content {
		if !strings.HasPrefix(key, "_") {
			return payload, nil
		}
	}
	if content == nil {
		// An empty or null payload decodes to a nil map, which cannot be written
		// to. Seed the format version: Kong rejects a payload without one.
		content = map[string]any{"_format_version": "3.0"}
	}
	content["upstreams"] = []any{map[string]any{"name": stubUpstreamName}}
	out, err := yaml.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshaling dbless configuration with the stub upstream: %w", err)
	}
	return out, nil
}
