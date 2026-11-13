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
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/Kong/ai-deck-converter/convert"
	"github.com/samber/lo"
	yaml "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// onPremAIGatewayGVK is the kind a configuration entity's aiGatewayRef points at
// once it targets an on-prem AI Gateway.
var onPremAIGatewayGVK = metav1.GroupVersionKind(aigatewayv1alpha1.GroupVersion.WithKind("OnPremAIGateway"))

// defaultMinRuntimeVersion is the minimum AI Gateway runtime version declared for
// rendered configurations when the caller does not specify one: the oldest release
// that must run the pushed output. Keep it unchanged to preserve the rendered
// configuration of existing OnPremAIGateways that leave spec.minRuntimeVersion unset.
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
			// Entities are listed regardless of their namespace: the instance's cache is
			// scoped to the namespaces of spec.watchNamespaces (see the OnPremAIGateway
			// controller), so an entity from another namespace reaches this loop only when
			// that namespace is watched, together with the Secrets it references. The
			// gateway's namespace must also grant that namespace the reference, or the
			// entity is left out of the pushed configuration.
			if err := CheckCrossNamespaceRef(ctx, cl, obj, gw); err != nil {
				statuses = append(statuses, EntityStatus{
					Obj: obj,
					Err: fmt.Errorf("%T %s: %w", obj, client.ObjectKeyFromObject(obj), err),
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

// CheckCrossNamespaceRef reports whether obj may reference the on-prem gateway
// gw: an entity outside the gateway's namespace needs a KongReferenceGrant in
// the gateway's namespace that allows its kind, from its namespace, to reference
// the OnPremAIGateway. An entity in the gateway's own namespace needs none.
// The on-prem reconcilers use it too, so that the ResolvedRefs condition and
// the translation agree.
func CheckCrossNamespaceRef(ctx context.Context, cl client.Client, obj client.Object, gw types.NamespacedName) error {
	if gw.Namespace == "" || obj.GetNamespace() == gw.Namespace {
		return nil
	}
	fromGVK, err := cl.GroupVersionKindFor(obj)
	if err != nil {
		return fmt.Errorf("resolving the kind of %s: %w", client.ObjectKeyFromObject(obj), err)
	}
	return crossnamespace.CheckKongReferenceGrantForResource(
		ctx,
		cl,
		obj.GetNamespace(),
		gw.Namespace,
		gw.Name,
		metav1.GroupVersionKind(fromGVK),
		onPremAIGatewayGVK,
	)
}

// credentialEntityStatuses turns the per-credential outcome of a consumer's translation into
// document entity statuses. Credentials are nested in their parent consumer, so their status
// carries no document entity of its own: a credential whose own translation failed is
// reported on the credential (its Err) instead of failing the consumer, and the consumer
// keeps rendering its healthy credentials.
//
// consumer is the consumer the credentials belong to, and consumerErr its failure, nil when it
// made it into the document: credentials are rendered through their consumer, so a consumer
// left out leaves them out of the pushed configuration too. Reporting them failed is what
// keeps their status fresh - the reporter only notifies the entities of the current sync, so
// a credential dropped from the status set keeps whatever condition it had (often
// Programmed=True).
//
// The status scheme has one failure reason, so the message carries the cause: a credential
// that translates fine but is left out with its consumer reports the consumer's failure.
func credentialEntityStatuses(
	translations []aiconfigurationv1alpha1.CredentialTranslation,
	consumer *aiconfigurationv1alpha1.AIGatewayConsumer,
	consumerErr error,
) []EntityStatus {
	statuses := make([]EntityStatus, 0, len(translations))
	for _, t := range translations {
		status := EntityStatus{Obj: t.Obj}
		switch {
		case t.Err != nil:
			status.Err = fmt.Errorf("converting %T %s: %w", t.Obj, client.ObjectKeyFromObject(t.Obj), t.Err)
		case consumerErr != nil:
			status.Err = fmt.Errorf("its AIGatewayConsumer %s failed: %w", client.ObjectKeyFromObject(consumer), consumerErr)
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// entityRef identifies a referenced entity: its kind, and the name the referencing
// entities use for it.
type entityRef struct {
	Kind string
	Name string
}

// entityOutcomes maps the translated entities to their outcome: nil when the entity
// translated, its error when it failed. The error lets a referencing entity carry the
// root cause of its own exclusion (see ReferencedEntityExcludedError).
type entityOutcomes map[entityRef]error

// record stores the outcome of the entity of s. A datastore is keyed by its CR key
// (namespace/name), because the referencing entities reference the CR; the other kinds by
// entity name. A success wins over a failure of another CR with the same key, because the
// document then holds the entity.
func (o entityOutcomes) record(s EntityStatus) {
	// Infallible for every aiconfiguration kind that translateKind translates.
	e, ok := s.Obj.(interface {
		GetTypeName() string
		GetKonnectName() string
	})
	if !ok {
		return
	}
	ref := entityRef{Kind: e.GetTypeName(), Name: e.GetKonnectName()}
	if _, ok := s.Obj.(*aiconfigurationv1alpha1.AIGatewayDatastore); ok {
		ref.Name = client.ObjectKeyFromObject(s.Obj).String()
	}
	if prev, ok := o[ref]; ok && (prev == nil || s.Err != nil) {
		return
	}
	o[ref] = s.Err
}

// ReferencedEntityExcludedError reports an entity that references an entity excluded from
// the document. Unwrap returns the error of the referenced entity, so that the
// classification of the root cause (see IsEntityScopedFailure) applies to the referencing
// entity too.
type ReferencedEntityExcludedError struct {
	// Kind is the kind of the referenced entity.
	Kind string
	// Name is the entity name of the referenced entity.
	Name string
	// Cause is the translation error of the referenced entity. It is nil when no entity
	// of that name was translated.
	Cause error
}

func (e *ReferencedEntityExcludedError) Error() string {
	return fmt.Sprintf(
		"referenced %s %q was excluded from the document: its own translation failed, or it references an excluded server",
		e.Kind, e.Name,
	)
}

// Unwrap returns the translation error of the referenced entity.
func (e *ReferencedEntityExcludedError) Unwrap() error {
	return e.Cause
}

// IsEntityScopedFailure reports whether err only excludes its entity from the document
// and is not a gateway-level translation failure. The entity reports err on its own
// status, and the gateway's aggregated ConfigurationTranslated condition treats it as a
// warning, so the rest of the configuration is still pushed and the gateway, and its data
// planes, become Ready. This applies to a model alias conflict, to a cross-namespace
// reference that no KongReferenceGrant allows, and to the entities that reference an
// entity excluded for one of these.
func IsEntityScopedFailure(err error) bool {
	if _, ok := errors.AsType[*ModelAliasConflictError](err); ok {
		return true
	}
	return crossnamespace.IsReferenceNotGranted(err)
}

// checkTranslatedRefs fails the referencing entity when any referenced name is missing from
// the set of entities of that kind that translated successfully: resolveEntityNames only
// checks the referenced entity's existence, so without this a referenced entity excluded for
// its own failed translation would let the referencing entity report success while the
// converter drops it from the pushed payload (or silently omits the reference).
func checkTranslatedRefs(outcomes entityOutcomes, kind string, names []string) error {
	for _, name := range names {
		if err, ok := outcomes[entityRef{Kind: kind, Name: name}]; !ok || err != nil {
			return &ReferencedEntityExcludedError{Kind: kind, Name: name, Cause: err}
		}
	}
	return nil
}

// checkTranslatedACLs checks both ACL name lists: every entry comes from a resolved
// consumer-group CR reference, so each must point at a translated AIGatewayConsumerGroup.
func checkTranslatedACLs(outcomes entityOutcomes, acls aigw.ACLs) error {
	return cmp.Or(
		checkTranslatedRefs(outcomes, "AIGatewayConsumerGroup", acls.Allow),
		checkTranslatedRefs(outcomes, "AIGatewayConsumerGroup", acls.Deny),
	)
}

// checkTranslatedDatastores is checkTranslatedRefs for the datastore references, which the caller
// passes as the referencing CR's datastore keys (see entityOutcomes.record).
func checkTranslatedDatastores(outcomes entityOutcomes, keys []client.ObjectKey) error {
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, key.String())
	}
	return checkTranslatedRefs(outcomes, "AIGatewayDatastore", names)
}

// modelAliases mirrors ai-deck-converter's extractModelAliases: the model selector's
// values when set, the model's entity name otherwise. The converter renders one
// ai_models entity per alias, named after it, so these are the names the data plane
// enforces uniqueness on.
func modelAliases(m *aigw.Model) []string {
	values := m.Config.Route.Model.Values
	if len(values) == 0 {
		return []string{m.Name}
	}
	// lo.Uniq keeps the first occurrence of each value, mirroring the
	// converter's extractModelAliases: a repeated value renders one ai_models
	// entity, not two identically named ones.
	return lo.Uniq(values)
}

// ModelAliasConflictError reports a model whose selector alias (or entity name, when no
// selector is set) is already claimed by another model on the same route.
//
// It is a per-entity routing conflict, not a gateway-level translation failure: the
// later model is excluded from the document and reports the conflict on its own status,
// while the rest of the configuration translated and pushed fine. The reporting layer
// detects this type and treats it as a warning on the gateway's aggregated
// ConfigurationTranslated condition, which therefore stays True and lets the gateway —
// and its data planes — become Ready (see ai-deck-converter#248).
//
// Two models sharing an alias across DIFFERENT routes are not a conflict: the converter
// renders one shared ai_models row for them and each model keeps its own route and
// plugin.
type ModelAliasConflictError struct {
	// Alias is the contested selector alias.
	Alias string
	// Route is the name of the rendered route both models land on.
	Route string
	// Paths are the route paths that the excluded model configures.
	Paths []string
	// First is the name of the AIGatewayModel that keeps the alias (the earliest created).
	First string
}

func (e *ModelAliasConflictError) Error() string {
	var paths string
	if len(e.Paths) > 0 {
		paths = fmt.Sprintf(" (paths %s)", strings.Join(e.Paths, ", "))
	}
	return fmt.Sprintf(
		"duplicate model alias %q on route %q%s: AIGatewayModel %q already uses it there.",
		e.Alias, e.Route, paths, e.First,
	)
}

// BuildDocument assembles the aigw.Document for the given OnPremAIGateway, translating every
// aiconfiguration entity kind pointing at it. Alongside the document it returns the per-entity
// translation status (see EntityStatus), so that the caller can report each entity's outcome on
// the entity's status.
//
// The statuses cover the AIGatewayConsumerCredentials embedded in the translated consumers as
// well: they have no document entity of their own (aigw.Consumer nests them), so the consumer's
// own translation reports them, and one broken credential does not fail its consumer.
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
	opts convert.Options,
) (*aigw.Document, []EntityStatus, error) {
	doc := &aigw.Document{}
	var statuses []EntityStatus
	// The outcome of every entity of the kinds translated so far, filled from their statuses
	// and checked by the referencing entities: the existence-only resolvers (resolveEntityNames)
	// only check the referenced entity's existence and same-gateway membership, so without it
	// a referenced entity that failed its own translation (e.g. a missing or unlabeled
	// secretRef Secret) would leave its referent reporting success while the converter drops
	// the referent from the pushed payload. Cross-kind dependencies are ordered first in the
	// translate list below so the referent's check sees the complete set; the one same-kind
	// dependency (MCPServer sources) is validated by the fixpoint pass after the loop.
	outcomes := make(entityOutcomes)
	// translatedMCPCRByName maps a translated MCPServer's entity name to its original CR, so
	// the same-kind sources post-pass can attribute a dropped listener's failure to the CR
	// whose EntityStatus already reported success (it stores the same pointer
	// translateKind put into the statuses).
	translatedMCPCRByName := make(map[string]client.Object)

	// credentialStatuses holds the per-credential outcome collected while translating the
	// consumers: credentials are not a document kind of their own (they are nested in their
	// parent consumer), so no translateKind call reports them.
	credentialStatuses := []EntityStatus{}

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

	// Model CRs that translated successfully, index-aligned with doc.Models: the
	// alias adjudication post-pass maps each rendered model back to its CR to
	// pick the winner per contested alias and patch the losers' statuses.
	// translatedModelCRs[i] is the CR of doc.Models[i].
	translatedModelCRs := make([]*aiconfigurationv1alpha1.AIGatewayModel, 0, 4)

	// The list is ordered so every kind translates after all the kinds it references:
	// checkTranslatedRefs inside the converting wrappers relies on the referenced kinds'
	// translated-name sets being complete by then. The one same-kind dependency
	// (MCPServer -> MCPServer sources) can't be ordered this way, hence the post-pass below.
	for _, translate := range []func(context.Context) ([]EntityStatus, error){
		translateKind[aiconfigurationv1alpha1.AIGatewayDatastoreList](
			cl, gw, index.IndexFieldAIGatewayDatastoreOnOnPremAIGatewayRef,
			func(d *aiconfigurationv1alpha1.AIGatewayDatastore, ctx context.Context, cl client.Client) (*aigw.Datastore, error) {
				datastore, err := d.ToAIGWDatastore(ctx, cl)
				if err != nil {
					return nil, err
				}
				// The converter resolves a datastore reference by name, into one map, so two
				// datastores sharing a name would silently leave the later one unreachable.
				if slices.ContainsFunc(doc.Datastores, func(other aigw.Datastore) bool {
					return other.Name == datastore.Name
				}) {
					return nil, fmt.Errorf(
						"duplicate datastore name %q: entity names must be unique across AIGatewayDatastore CRs, because the referencing entities resolve a datastore by name",
						datastore.Name,
					)
				}
				return datastore, nil
			},
			&doc.Datastores),
		translateKind[aiconfigurationv1alpha1.AIGatewayModelProviderList](
			cl, gw, index.IndexFieldAIGatewayModelProviderOnOnPremAIGatewayRef,
			func(p *aiconfigurationv1alpha1.AIGatewayModelProvider, ctx context.Context, cl client.Client) (*aigw.Provider, error) {
				provider, err := p.ToAIGWProvider(ctx, cl)
				if err == nil {
					err = checkTranslatedDatastores(outcomes, aiconfigurationv1alpha1.AIGatewayModelProviderRefsToAIGatewayDatastore(p))
				}
				if err != nil {
					return nil, err
				}
				return provider, nil
			},
			&doc.ModelProviders),
		translateKind[aiconfigurationv1alpha1.AIGatewayPolicyList](
			cl, gw, index.IndexFieldAIGatewayPolicyOnOnPremAIGatewayRef,
			func(p *aiconfigurationv1alpha1.AIGatewayPolicy, ctx context.Context, cl client.Client) (*aigw.Policy, error) {
				policy, err := p.ToAIGWPolicy(ctx, cl)
				if err == nil {
					err = checkTranslatedDatastores(outcomes, aiconfigurationv1alpha1.AIGatewayPolicyRefsToAIGatewayDatastore(p))
				}
				if err != nil {
					return nil, err
				}
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
				if err == nil {
					err = checkTranslatedRefs(outcomes, "AIGatewayPolicy", group.Policies)
				}
				if err != nil {
					return nil, err
				}
				return group, nil
			},
			&doc.ConsumerGroups),
		translateKind[aiconfigurationv1alpha1.AIGatewayAuthStrategyList](
			cl, gw, index.IndexFieldAIGatewayAuthStrategyOnOnPremAIGatewayRef,
			func(a *aiconfigurationv1alpha1.AIGatewayAuthStrategy, ctx context.Context, cl client.Client) (*aigw.AuthStrategy, error) {
				authStrategy, err := a.ToAIGWAuthStrategy(ctx, cl)
				if err == nil {
					err = checkTranslatedDatastores(outcomes, aiconfigurationv1alpha1.AIGatewayAuthStrategyRefsToAIGatewayDatastore(a))
				}
				if err != nil {
					return nil, err
				}
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
					checkTranslatedRefs(outcomes, "AIGatewayPolicy", model.Policies),
					checkTranslatedRefs(outcomes, "AIGatewayAuthStrategy", model.Access.AuthStrategies),
					checkTranslatedACLs(outcomes, model.Access.ACLs),
					checkTranslatedDatastores(outcomes, aiconfigurationv1alpha1.AIGatewayModelRefsToAIGatewayDatastore(m)),
				); err != nil {
					return nil, err
				}
				for i := range model.TargetModels {
					if err := checkTranslatedRefs(
						outcomes, "AIGatewayModelProvider",
						[]string{model.TargetModels[i].Provider},
					); err != nil {
						return nil, err
					}
				}
				// ToAIGWModel resolves the semantic balancer's embeddings provider to the
				// referenced provider's entity name; check it made it into the document.
				var embeddingsProvider []string
				if b := model.Config.Balancer; b != nil {
					embeddings, _ := b.Fields["embeddings"].(map[string]any)
					if p, _ := embeddings["provider"].(string); p != "" {
						embeddingsProvider = []string{p}
					}
				}
				if err := checkTranslatedRefs(outcomes, "AIGatewayModelProvider", embeddingsProvider); err != nil {
					return nil, err
				}
				translatedModelCRs = append(translatedModelCRs, m)
				return model, nil
			},
			&doc.Models),
		translateKind[aiconfigurationv1alpha1.AIGatewayConsumerList](
			cl, gw, index.IndexFieldAIGatewayConsumerOnOnPremAIGatewayRef,
			// The consumer's credential lookup uses this package's index for
			// AIGatewayConsumerCredential -> AIGatewayConsumer: the field name is passed in
			// because the api package cannot import internal/utils/index without a cycle.
			func(c *aiconfigurationv1alpha1.AIGatewayConsumer, ctx context.Context, cl client.Client) (*aigw.Consumer, error) {
				consumer, credentialTranslations, err := c.ToAIGWConsumer(ctx, cl, index.IndexFieldAIGatewayConsumerCredentialOnAIGatewayConsumerRef)
				if err == nil {
					err = cmp.Or(
						checkTranslatedRefs(outcomes, "AIGatewayPolicy", consumer.Policies),
						checkTranslatedRefs(outcomes, "AIGatewayConsumerGroup", consumer.ConsumerGroups),
					)
				}
				// Reported before the early return below: credentials are rendered through
				// their consumer, so one that fails or is excluded leaves them out of the
				// pushed configuration as well and they must be reported failed.
				credentialStatuses = append(credentialStatuses, credentialEntityStatuses(credentialTranslations, c, err)...)
				if err != nil {
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
					checkTranslatedRefs(outcomes, "AIGatewayPolicy", agent.Policies),
					checkTranslatedACLs(outcomes, agent.Access.ACLs),
					checkTranslatedRefs(outcomes, "AIGatewayAuthStrategy", agent.Access.AuthStrategies),
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
					checkTranslatedRefs(outcomes, "AIGatewayPolicy", mcpServer.Policies),
					checkTranslatedACLs(outcomes, mcpServer.Access.ACLs),
					checkTranslatedACLs(outcomes, mcpServer.Access.DefaultToolACLs),
					checkTranslatedRefs(outcomes, "AIGatewayAuthStrategy", mcpServer.Access.AuthStrategies),
					checkTranslatedDatastores(outcomes, aiconfigurationv1alpha1.AIGatewayMCPServerRefsToAIGatewayDatastore(s)),
				); err != nil {
					return nil, err
				}
				for i := range mcpServer.Tools {
					if err := checkTranslatedACLs(
						outcomes, mcpServer.Tools[i].Access.ACLs,
					); err != nil {
						return nil, err
					}
				}
				name := s.GetKonnectName()
				if _, dup := translatedMCPCRByName[name]; dup {
					return nil, fmt.Errorf(
						"duplicate AIGatewayMCPServer entity name %q: entity names must be unique across AIGatewayMCPServer CRs",
						name,
					)
				}
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
				return c.ToAIGWCertificate(ctx, cl)
			},
			&doc.Certificates),
		translateKind[aiconfigurationv1alpha1.AIGatewaySNIList](
			cl, gw, index.IndexFieldAIGatewaySNIOnOnPremAIGatewayRef,
			func(s *aiconfigurationv1alpha1.AIGatewaySNI, ctx context.Context, cl client.Client) (*aigw.SNI, error) {
				sni, err := s.ToAIGWSNI(ctx, cl)
				if err != nil {
					return nil, err
				}
				if err := checkTranslatedRefs(outcomes, "AIGatewayCertificate", []string{sni.Certificate}); err != nil {
					return nil, err
				}
				return sni, nil
			},
			&doc.SNIs),
	} {
		s, err := translate(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, status := range s {
			outcomes.record(status)
		}
		statuses = append(statuses, s...)
	}

	// A consumer whose conversion failed before it produced credential translations
	// left its credentials unreported: report them with the consumer's error here, for
	// the same reason - they are rendered through the consumer and must not keep a
	// stale Programmed condition. A consumer that failed after resolving its
	// credentials already reported them, so this post-pass repeats that status.
	for _, s := range statuses {
		consumer, ok := s.Obj.(*aiconfigurationv1alpha1.AIGatewayConsumer)
		if !ok || s.Err == nil || consumer.Namespace == gw.Namespace {
			continue
		}
		var creds aiconfigurationv1alpha1.AIGatewayConsumerCredentialList
		// The same listing the consumer's own translation does. A failure here cannot be
		// reported on the credentials it failed to read: the consumer's error is already
		// reported, and the next sync retries.
		if err := cl.List(ctx, &creds,
			client.InNamespace(consumer.Namespace),
			client.MatchingFields{
				index.IndexFieldAIGatewayConsumerCredentialOnAIGatewayConsumerRef: consumer.Namespace + "/" + consumer.Name,
			},
		); err != nil {
			continue
		}
		for i := range creds.Items {
			credentialStatuses = append(credentialStatuses, EntityStatus{
				Obj: &creds.Items[i],
				Err: fmt.Errorf("its AIGatewayConsumer %s failed: %w", client.ObjectKeyFromObject(consumer), s.Err),
			})
		}
	}
	statuses = append(statuses, credentialStatuses...)

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
			excluded := checkTranslatedRefs(outcomes, "AIGatewayMCPServer", m.Config.Sources)
			if excluded == nil {
				kept = append(kept, m)
				continue
			}
			dropped = true
			// Assigned, not recorded: the listener translated, and its exclusion must
			// replace that success so that a listener sourcing it keeps the cause too.
			outcomes[entityRef{Kind: "AIGatewayMCPServer", Name: m.Name}] = excluded
			cr, ok := translatedMCPCRByName[m.Name]
			if !ok {
				continue
			}
			for i := range statuses {
				if statuses[i].Obj == cr {
					statuses[i].Err = excluded
				}
			}
		}
		doc.MCPServers = kept
	}

	// Same-route alias conflicts need the converter's route grouping, so they
	// are resolved on a probe render. See resolveModelAliasRouteConflicts.
	// The resolution indexes both slices together. A mismatch is a bug, so
	// fail the sync instead of a panic or a wrong exclusion.
	if len(translatedModelCRs) != len(doc.Models) {
		return nil, nil, fmt.Errorf(
			"internal error: %d translated AIGatewayModel CRs for %d document models",
			len(translatedModelCRs), len(doc.Models),
		)
	}
	resolveModelAliasRouteConflicts(doc, statuses, translatedModelCRs, opts)

	return doc, statuses, nil
}

// sameRouteAliasConflictRe matches the converter's same-route alias warning.
// The warning has no structured details, and ROUTING_CONFLICT has other
// emitters, so the match uses severity, code, and message, as agreed with the
// converter team. TestCollectAliasConflicts_ConverterContract fails when a
// converter bump changes the message. If a change is not caught, the conflict
// stays a plain warning: the payload is still valid, so this fails open.
var sameRouteAliasConflictRe = regexp.MustCompile(
	`^model ("(?:[^"\\]|\\.)*") uses the alias ("(?:[^"\\]|\\.)*") on the route ("(?:[^"\\]|\\.)*"), but model ("(?:[^"\\]|\\.)*") already uses it there\.`,
)

// aliasConflict is one same-route alias conflict that the converter reported:
// claimer uses alias on route, and owner used it there first.
type aliasConflict struct {
	route   string
	alias   string
	owner   string
	claimer string
}

// collectAliasConflicts returns the same-route alias conflicts in issues.
func collectAliasConflicts(issues []convert.ConversionIssue) []aliasConflict {
	var conflicts []aliasConflict
	for _, issue := range issues {
		if issue.Severity != convert.Warning || issue.Code != convert.RoutingConflict {
			continue
		}
		m := sameRouteAliasConflictRe.FindStringSubmatch(issue.Reason)
		if len(m) == 0 {
			continue
		}
		// The converter formats the values with %q.
		var fields [4]string
		ok := true
		for i := range fields {
			var err error
			if fields[i], err = strconv.Unquote(m[i+1]); err != nil {
				ok = false
			}
		}
		// The message and the structured source must name the same model.
		if !ok || fields[0] != issue.Source.Name {
			continue
		}
		conflicts = append(conflicts, aliasConflict{
			claimer: fields[0], alias: fields[1], route: fields[2], owner: fields[3],
		})
	}
	return conflicts
}

// resolveModelAliasRouteConflicts excludes each model that uses an alias that
// an earlier-created model already uses on the same route. Two such models
// make two ai-proxy plugins with the same route and ai_models scope, and the
// data plane then sends all traffic to one of them. The converter accepts
// this for compatibility (it only warns), so the operator enforces it.
// Models that share an alias on different routes stay: they share one
// ai_models row and keep their own routes.
//
// The function sorts doc.Models by creation time (ties: namespace/name), so a
// new model cannot take an alias from a programmed model. The converter
// claims aliases in document order, so the owner in each warning is then the
// earlier-created model.
//
// The resolution repeats until a probe render has no conflicts. In each pass,
// a claimer loses only if its owner is not a claimer too: such an owner stays
// in all later passes, so the loss is final. A claimer whose owner can still
// lose waits for the next pass, because an excluded model must not exclude
// other models. The earliest claimer always has an owner that is not a
// claimer, so each pass excludes at least one model and the loop ends.
//
// The function changes doc.Models and the Err of the excluded models' statuses
// in place.
func resolveModelAliasRouteConflicts(
	doc *aigw.Document,
	statuses []EntityStatus,
	crs []*aiconfigurationv1alpha1.AIGatewayModel,
	opts convert.Options,
) {
	if len(doc.Models) < 2 {
		return
	}
	sortModelsByCreation(doc.Models, crs)
	// A conflict needs one alias on two enabled models. Most gateways have
	// none, so skip the probe render, which is a full converter run.
	if !hasSharedAlias(doc.Models) {
		return
	}

	for {
		probe, idxByName := buildProbeDoc(doc)
		_, issues, err := RenderDBLessYAML(probe, opts)
		if err != nil {
			// The probe differs from doc only in model names, and no converter
			// failure depends on a name, so the caller's render reports the
			// same error.
			return
		}
		conflicts := collectAliasConflicts(issues)

		claimers := make(map[string]bool, len(conflicts))
		for _, c := range conflicts {
			claimers[c.claimer] = true
		}
		losers := make(map[int]*ModelAliasConflictError)
		for _, c := range conflicts {
			owner, ownerOK := idxByName[c.owner]
			claimer, claimerOK := idxByName[c.claimer]
			if !ownerOK || !claimerOK || claimers[c.owner] {
				continue
			}
			if _, ok := losers[claimer]; !ok {
				losers[claimer] = &ModelAliasConflictError{
					Alias: c.alias,
					Route: c.route,
					Paths: doc.Models[claimer].Config.Route.Paths,
					First: crs[owner].Name,
				}
			}
		}
		if len(losers) == 0 {
			return
		}

		keptModels := make([]aigw.Model, 0, len(doc.Models)-len(losers))
		keptCRs := make([]*aiconfigurationv1alpha1.AIGatewayModel, 0, len(crs)-len(losers))
		for i := range doc.Models {
			conflict, loser := losers[i]
			if !loser {
				keptModels = append(keptModels, doc.Models[i])
				keptCRs = append(keptCRs, crs[i])
				continue
			}
			// The status reported success during translation: patch it in
			// place, as the MCPServer sources pass does.
			cr := crs[i]
			for j := range statuses {
				if statuses[j].Obj == cr {
					statuses[j].Err = fmt.Errorf("converting %T %s: %w", cr, client.ObjectKeyFromObject(cr), conflict)
				}
			}
		}
		doc.Models = keptModels
		crs = keptCRs
	}
}

// hasSharedAlias reports whether two enabled models use one alias. Disabled
// models render nothing, so they cannot conflict.
func hasSharedAlias(models []aigw.Model) bool {
	seen := make(map[string]bool, len(models))
	for i := range models {
		if m := &models[i]; m.Enabled == nil || *m.Enabled {
			for _, alias := range modelAliases(m) {
				if seen[alias] {
					return true
				}
				seen[alias] = true
			}
		}
	}
	return false
}

// sortModelsByCreation sorts models and their index-aligned crs by the CR
// creation time, with namespace/name as the tie-break.
func sortModelsByCreation(models []aigw.Model, crs []*aiconfigurationv1alpha1.AIGatewayModel) {
	order := make([]int, len(models))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		ca, cb := crs[a], crs[b]
		return cmp.Or(
			ca.CreationTimestamp.Compare(cb.CreationTimestamp.Time),
			cmp.Compare(ca.Namespace, cb.Namespace),
			cmp.Compare(ca.Name, cb.Name),
		)
	})
	sortedModels := make([]aigw.Model, len(models))
	sortedCRs := make([]*aiconfigurationv1alpha1.AIGatewayModel, len(crs))
	for i, j := range order {
		sortedModels[i], sortedCRs[i] = models[j], crs[j]
	}
	copy(models, sortedModels)
	copy(crs, sortedCRs)
}

// buildProbeDoc returns a copy of doc in which each model has a unique name,
// and the index in doc of each probe model name. The converter names models in
// its warnings by entity name, but two CRs can set the same spec name.
//
// A renamed model gets its aliases as explicit selector values: without a
// selector, the entity name is the alias. The converter renders explicit
// values that equal the name the same as no values, so the probe has the
// same routes and conflicts as doc.
func buildProbeDoc(doc *aigw.Document) (*aigw.Document, map[string]int) {
	probe := *doc
	probe.Models = slices.Clone(doc.Models)

	// Names are not restricted, so a new name must not match any model name.
	used := make(map[string]bool, len(probe.Models))
	for _, m := range probe.Models {
		used[m.Name] = true
	}
	idxByName := make(map[string]int, len(probe.Models))
	for i := range probe.Models {
		m := &probe.Models[i]
		if _, dup := idxByName[m.Name]; dup {
			m.Config.Route.Model.Values = modelAliases(m)
			name := m.Name
			for n := i; used[name]; n++ {
				name = fmt.Sprintf("%s#%d", m.Name, n)
			}
			used[name] = true
			m.Name = name
		}
		idxByName[m.Name] = i
	}
	return &probe, idxByName
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
	// The stub carries no id, unlike every entity the converter renders: the AI
	// Gateway accepts an id-less upstream and assigns one itself even under the
	// payload's _transform: false, so no id has to be synthesized here (verified
	// by TestEmptyDBLessPayloadKeepsAIGatewayReady against a real data plane).
	content["upstreams"] = []any{map[string]any{"name": stubUpstreamName}}
	out, err := yaml.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshaling dbless configuration with the stub upstream: %w", err)
	}
	return out, nil
}
