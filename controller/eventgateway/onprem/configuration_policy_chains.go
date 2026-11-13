package onprem

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
)

type invalidPolicyChainsError struct {
	problems []string
}

func (e invalidPolicyChainsError) Error() string {
	return "Policy chains prevent applying the complete gateway configuration: " + strings.Join(e.problems, "; ")
}

// chainObject is a policy chain: an EventGatewayVirtualClusterPolicyChain or
// an EventGatewayListenerPolicyChain.
type chainObject interface {
	policychain.Chain
	entityObject
	GetParentGVK() schema.GroupVersionKind
}

// policyRef is a reference of a chain to a policy it orders.
type policyRef struct {
	kind, name, namespace string
}

// policyChainScope is what applying the chains of a kind of parent takes: a
// chain orders the policies of a virtual cluster, or of a listener, in the
// configuration. C is the chain, E the entity of a policy in gw-admin.
type policyChainScope[C chainObject, E any] struct {
	// policyKind is the kind of the policies the chain orders; the kind of
	// its parent, the parent itself and the annotation recording claims on
	// it come from the chain (see policychain.Scope).
	policyKind string
	// parentWhat and policyWhat name the parent and the policies in messages,
	// e.g. "virtual cluster" and "cluster policy"; listWhat names the
	// policies the chain must list, e.g. "cluster-level policy".
	parentWhat, policyWhat, listWhat string
	newPolicy                        func() entityWithParent
	// chains lists the chains of the parents (see childrenOf).
	chains func(ctx context.Context, c *configuration, cl client.Client, p parents) ([]C, map[client.ObjectKey]int, error)
	// policies returns the policies the chain orders, nil when it doesn't
	// supply them.
	policies func(C) []policyRef
	// listPolicies lists the policies of the parent.
	listPolicies func(ctx context.Context, cl client.Client, parent client.ObjectKey) ([]entityWithParent, error)
	// entities returns the path, and the list, of the policies of the parent
	// of index i in the configuration, and the id of the parent.
	entities func(c *configuration, i int) (string, *[]E, gwadminapi.UUIDv4)
	id       func(E) gwadminapi.UUIDv4
	// validate, when set, checks the complete ordered set of the policies the
	// chain declares, from the objects read for it in the declared order, and
	// returns why it can't be applied, or "".
	validate func(chain C, refs []policyRef, policies []entityWithParent) string
	// ordered returns the parents this scope records as ordered, so that the
	// comparison of the configuration with gw-admin is order-sensitive for
	// their policy list alone.
	ordered func(*configuration) *[]uuid.UUID
}

// clusterPolicyChains order the cluster policies of virtual clusters.
var clusterPolicyChains = policyChainScope[*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, gwadminapi.EventGatewayClusterPolicyOnPrem]{
	policyKind: "EventGatewayVirtualClusterPolicy",
	parentWhat: "virtual cluster",
	policyWhat: "cluster policy",
	listWhat:   "cluster-level policy",
	newPolicy:  func() entityWithParent { return &configurationv1alpha1.EventGatewayVirtualClusterPolicy{} },
	chains: func(ctx context.Context, c *configuration, cl client.Client, p parents) ([]*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, map[client.ObjectKey]int, error) {
		var list configurationv1alpha1.EventGatewayVirtualClusterPolicyChainList
		chains, parentIndex, _, err := childrenOf(ctx, c, cl, &list, &list.Items,
			index.IndexFieldEventGatewayVirtualClusterPolicyChainOnEventGatewayVirtualClusterRef, p)
		return chains, parentIndex, err
	},
	policies: func(chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) []policyRef {
		return policyRefs(chain.Spec.APISpec.Policies, func(r configurationv1alpha1.EventGatewayVirtualClusterPolicyRef) policyRef {
			return policyRef{kind: r.Kind, name: r.Name, namespace: r.Namespace}
		})
	},
	listPolicies: func(ctx context.Context, cl client.Client, parent client.ObjectKey) ([]entityWithParent, error) {
		var policies configurationv1alpha1.EventGatewayVirtualClusterPolicyList
		if err := cl.List(ctx, &policies, client.MatchingFields{
			index.IndexFieldEventGatewayVirtualClusterPolicyOnEventGatewayVirtualClusterRef: parent.String(),
		}); err != nil {
			return nil, err
		}
		return entityWithParents(policies.Items), nil
	},
	entities: func(c *configuration, i int) (string, *[]gwadminapi.EventGatewayClusterPolicyOnPrem, gwadminapi.UUIDv4) {
		vc := &c.entities.VirtualClusters[i]
		return fmt.Sprintf("virtual_clusters.%d.cluster_policies", i), &vc.ClusterPolicies, vc.Id
	},
	id:      func(p gwadminapi.EventGatewayClusterPolicyOnPrem) gwadminapi.UUIDv4 { return p.Id },
	ordered: func(c *configuration) *[]uuid.UUID { return &c.orderedPolicies },
}

// listenerPolicyChains order the policies of listeners.
var listenerPolicyChains = policyChainScope[*configurationv1alpha1.EventGatewayListenerPolicyChain, gwadminapi.EventGatewayListenerPolicyOnPrem]{
	policyKind: "EventGatewayListenerPolicy",
	parentWhat: "listener",
	policyWhat: "listener policy",
	listWhat:   "listener policy",
	newPolicy:  func() entityWithParent { return &configurationv1alpha1.EventGatewayListenerPolicy{} },
	chains: func(ctx context.Context, c *configuration, cl client.Client, p parents) ([]*configurationv1alpha1.EventGatewayListenerPolicyChain, map[client.ObjectKey]int, error) {
		var list configurationv1alpha1.EventGatewayListenerPolicyChainList
		chains, parentIndex, _, err := childrenOf(ctx, c, cl, &list, &list.Items,
			index.IndexFieldEventGatewayListenerPolicyChainOnEventGatewayListenerRef, p)
		return chains, parentIndex, err
	},
	policies: func(chain *configurationv1alpha1.EventGatewayListenerPolicyChain) []policyRef {
		return policyRefs(chain.Spec.APISpec.Policies, func(r configurationv1alpha1.EventGatewayListenerPolicyRef) policyRef {
			return policyRef{kind: r.Kind, name: r.Name, namespace: r.Namespace}
		})
	},
	listPolicies: func(ctx context.Context, cl client.Client, parent client.ObjectKey) ([]entityWithParent, error) {
		var policies configurationv1alpha1.EventGatewayListenerPolicyList
		if err := cl.List(ctx, &policies, client.MatchingFields{
			index.IndexFieldEventGatewayListenerPolicyOnEventGatewayListenerRef: parent.String(),
		}); err != nil {
			return nil, err
		}
		return entityWithParents(policies.Items), nil
	},
	entities: func(c *configuration, i int) (string, *[]gwadminapi.EventGatewayListenerPolicyOnPrem, gwadminapi.UUIDv4) {
		l := &c.entities.Listeners[i]
		return fmt.Sprintf("listeners.%d.policies", i), &l.Policies, l.Id
	},
	id:      func(p gwadminapi.EventGatewayListenerPolicyOnPrem) gwadminapi.UUIDv4 { return p.Id },
	ordered: func(c *configuration) *[]uuid.UUID { return &c.orderedPolicies },
}

// producePolicyChains order the produce policies of virtual clusters. They
// order only the produce list: the cluster-level, consume group ordering and
// claim of the same virtual cluster are independent.
var producePolicyChains = policyChainScope[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, gwadminapi.EventGatewayProducePolicyOnPrem]{
	policyKind: "EventGatewayVirtualClusterProducePolicy",
	parentWhat: "virtual cluster",
	policyWhat: "produce policy",
	listWhat:   "produce policy",
	newPolicy:  func() entityWithParent { return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{} },
	chains: func(ctx context.Context, c *configuration, cl client.Client, p parents) ([]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, map[client.ObjectKey]int, error) {
		var list configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainList
		chains, parentIndex, _, err := childrenOf(ctx, c, cl, &list, &list.Items,
			index.IndexFieldEventGatewayVirtualClusterProducePolicyChainOnEventGatewayVirtualClusterRef, p)
		return chains, parentIndex, err
	},
	policies: func(chain *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain) []policyRef {
		return policyRefs(chain.Spec.APISpec.Policies, func(r configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef) policyRef {
			return policyRef{kind: r.Kind, name: r.Name, namespace: r.Namespace}
		})
	},
	listPolicies: func(ctx context.Context, cl client.Client, parent client.ObjectKey) ([]entityWithParent, error) {
		var policies configurationv1alpha1.EventGatewayVirtualClusterProducePolicyList
		if err := cl.List(ctx, &policies, client.MatchingFields{
			index.IndexFieldEventGatewayVirtualClusterProducePolicyOnEventGatewayVirtualClusterRef: parent.String(),
		}); err != nil {
			return nil, err
		}
		return entityWithParents(policies.Items), nil
	},
	entities: func(c *configuration, i int) (string, *[]gwadminapi.EventGatewayProducePolicyOnPrem, gwadminapi.UUIDv4) {
		vc := &c.entities.VirtualClusters[i]
		return fmt.Sprintf("virtual_clusters.%d.produce_policies", i), &vc.ProducePolicies, vc.Id
	},
	id:       func(p gwadminapi.EventGatewayProducePolicyOnPrem) gwadminapi.UUIDv4 { return p.Id },
	validate: producePolicyOrder,
	ordered:  func(c *configuration) *[]uuid.UUID { return &c.orderedProducePolicies },
}

// policyRefs returns the references of a chain to its policies, nil when it
// doesn't supply them.
func policyRefs[R any](refs []R, of func(R) policyRef) []policyRef {
	if refs == nil {
		return nil
	}
	out := make([]policyRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, of(r))
	}
	return out
}

// entityWithParents returns the policies of a list.
func entityWithParents[T any, PT interface {
	*T
	entityWithParent
}](items []T) []entityWithParent {
	objs := make([]entityWithParent, 0, len(items))
	for i := range items {
		objs = append(objs, PT(&items[i]))
	}
	return objs
}

func (c *configuration) addClusterPolicyChains(ctx context.Context, cl client.Client, virtualClusters parents) error {
	return addPolicyChains(ctx, c, cl, virtualClusters, clusterPolicyChains)
}

func (c *configuration) addProducePolicyChains(ctx context.Context, cl client.Client, virtualClusters parents) error {
	return addPolicyChains(ctx, c, cl, virtualClusters, producePolicyChains)
}

func (c *configuration) addListenerPolicyChains(ctx context.Context, cl client.Client, listeners parents) error {
	return addPolicyChains(ctx, c, cl, listeners, listenerPolicyChains)
}

// addPolicyChains applies the chains of the parents to the configuration: the
// chain owning a parent orders its policies.
func addPolicyChains[C chainObject, E any](ctx context.Context, c *configuration, cl client.Client, ps parents, s policyChainScope[C, E]) error {
	before := len(c.excluded)
	// A chain left out doesn't hold back the configuration by itself, e.g. one
	// competing for a parent another chain owns: invalidChains, of the chains
	// owning their parent, does.
	defer func() {
		for i := before; i < len(c.excluded); i++ {
			c.excluded[i].blocks = false
		}
	}()
	chains, parentIndex, err := s.chains(ctx, c, cl, ps)
	if err != nil {
		return err
	}
	scopeParent, _, _, err := policychain.Scope(*new(C))
	if err != nil {
		return err
	}
	parentType := reflect.TypeOf(scopeParent)
	activeClaims := make(map[client.ObjectKey]string, len(ps.keys))
	for _, parent := range ps.keys {
		// A parent of another namespace no KongReferenceGrant lets reference
		// its own parent doesn't belong to the gateway: neither it nor its
		// chains hold back the configuration, and its claim isn't the
		// gateway's to release.
		if c.leftOutNotPermitted(parentType, parent) {
			continue
		}
		claim, err := releaseInactivePolicyChainClaim(ctx, cl, parent, s)
		if err != nil {
			return err
		}
		activeClaims[parent] = claim
	}
	for _, excluded := range c.excluded[before:] {
		chain := excluded.obj.(C)
		parent, ok := policychain.ParentKey(chain)
		// A chain of another namespace no KongReferenceGrant lets reference its
		// parent doesn't belong to the gateway: owning it, it keeps the default
		// order of its policies, as a chain whose claim is taken.
		if !ok || excluded.reason == eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason {
			continue
		}
		if claim, ok := activeClaims[parent]; ok && claim == policychain.ClaimValue(chain) {
			c.invalidChains = append(c.invalidChains, client.ObjectKeyFromObject(chain).String()+": "+excluded.message)
		}
	}
	slices.SortFunc(chains, byCreation[C])
	// owned are the parents a chain owns; notPermitted, the chains no
	// KongReferenceGrant lets order their policies, by parent.
	owned := map[client.ObjectKey]bool{}
	notPermitted := map[client.ObjectKey][]string{}
	for _, chain := range chains {
		status := entityStatus{obj: chain}
		// The parent the chain may claim; whether a KongReferenceGrant lets
		// a chain of another namespace claim it was checked when listing it.
		parent, err := policychain.ClaimParentKey(chain)
		if err != nil {
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, err.Error())
			continue
		}
		// Policies of another namespace no KongReferenceGrant lets it
		// reference: reported as such, like for any other entity. As against
		// Konnect, such a chain doesn't claim its parent.
		message, err := siblingsNotGranted(ctx, cl, chain)
		if err != nil {
			return err
		}
		if message != "" {
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityReferenceNotPermittedReason, message)
			notPermitted[parent] = append(notPermitted[parent], client.ObjectKeyFromObject(chain).String()+": "+message)
			continue
		}
		owner, err := policychain.Claim(ctx, cl, chain)
		if err != nil {
			return err
		}
		if owner != "" {
			message := fmt.Sprintf("%s %s is owned by policy chain %s; delete that chain before claiming it", chain.GetParentGVK().Kind, policychain.NameIn(parent, chain.GetNamespace()), owner)
			status.conditions = []metav1.Condition{entityCondition(chain, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain", message)}
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
			continue
		}
		// Only the chain holding the claim owns the parent: one that lost it
		// to a chain that can't order its policies doesn't release the push.
		owned[parent] = true
		status.conditions = []metav1.Condition{entityCondition(chain, "PolicyChainOwnerValid", metav1.ConditionTrue, "Owner", "This resource owns the "+chain.GetParentGVK().Kind+" policy chain")}
		ordered, problem, err := policyOrder(ctx, c, cl, chain, s)
		if err != nil {
			return err
		}
		path, list, parentID := s.entities(c, parentIndex[client.ObjectKeyFromObject(chain)])
		// Validation errors returned by gw-admin refer to array positions.
		// Move their source-object paths along with the reordered entities.
		sources := make(map[gwadminapi.UUIDv4]string, len(*list))
		for j, p := range *list {
			sources[s.id(p)] = c.objects[fmt.Sprintf("%s.%d", path, j)]
		}
		if problem == "" && (len(ordered) != len(*list) || slices.ContainsFunc(ordered, func(p E) bool {
			_, included := sources[s.id(p)]
			return !included
		})) {
			problem = "the " + s.policyWhat + " set changed during configuration assembly; waiting for a consistent configuration"
		}
		if problem != "" {
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, problem)
			c.invalidChains = append(c.invalidChains, client.ObjectKeyFromObject(chain).String()+": "+problem)
			continue
		}
		for j, p := range ordered {
			c.objects[fmt.Sprintf("%s.%d", path, j)] = sources[s.id(p)]
		}
		*list = ordered
		*s.ordered(c) = append(*s.ordered(c), parentID)
		c.included = append(c.included, status)
	}
	// A chain that can't order its policies holds back the configuration of a
	// parent no other chain owns: it would keep the default order of its
	// policies.
	for _, parent := range slices.SortedFunc(maps.Keys(notPermitted), compareKeys) {
		if !owned[parent] {
			c.invalidChains = append(c.invalidChains, notPermitted[parent]...)
		}
	}
	return nil
}

func compareKeys(a, b client.ObjectKey) int {
	return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
}

// producePolicyOrder checks the declared order of a produce chain against the
// gw-admin document contract: a root schemaValidation group is contiguous and
// its children follow it. It returns why the order can't be applied, or "".
// The declaration is checked with the objects read for it, without Konnect
// status: on-prem applications rely on the Kubernetes references alone.
func producePolicyOrder(
	chain *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain,
	refs []policyRef,
	policies []entityWithParent,
) string {
	// The produce chain API supports no cross-namespace reference: the
	// parent and every policy are in the namespace of the chain, whatever a
	// KongReferenceGrant allows.
	if ref := chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef; ref != nil &&
		ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != chain.Namespace {
		return "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster in the same namespace"
	}
	for _, ref := range refs {
		if ref.namespace != "" && ref.namespace != chain.Namespace {
			return fmt.Sprintf("policy %s/%s must be in the namespace of the chain: produce chain references are not cross-namespace", ref.namespace, ref.name)
		}
	}
	order := make([]string, 0, len(chain.Spec.APISpec.Policies))
	for _, ref := range chain.Spec.APISpec.Policies {
		order = append(order, ref.Name)
	}
	byName := make(map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, len(policies))
	for i, policy := range policies {
		produce, ok := policy.(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy)
		if !ok {
			return fmt.Sprintf("spec.apiSpec.policies[%d] is not an EventGatewayVirtualClusterProducePolicy", i)
		}
		byName[produce.Name] = produce
	}
	// The declaration lists every policy of the parent exactly once; when the
	// set changed while it was read, the chain can't be applied.
	if len(byName) != len(order) {
		return "the produce policy set changed during configuration assembly; waiting for a consistent configuration"
	}
	if err := configurationv1alpha1.ValidateProducePolicyChainOrder(order, byName); err != nil {
		return err.Error()
	}
	return ""
}

// On-prem deletion is not a remote operation and needs no finalizer. A stale
// or terminating owner releases its claim even when no replacement exists.
// The surviving UID-bound claim is returned without acquiring a new one.
func releaseInactivePolicyChainClaim[C chainObject, E any](ctx context.Context, cl client.Client, key client.ObjectKey, s policyChainScope[C, E]) (string, error) {
	parent, owner, annotation, err := policychain.Scope(*new(C))
	if err != nil {
		return "", err
	}
	if err := cl.Get(ctx, key, parent); err != nil {
		return "", fmt.Errorf("reading %s for policy-chain ownership: %w", s.parentWhat, err)
	}
	claim := parent.GetAnnotations()[annotation]
	if claim == "" {
		return "", nil
	}
	holder, err := policychain.Holder(ctx, cl, owner, key, claim)
	if err != nil {
		return "", err
	}
	if holder != nil && holder.GetDeletionTimestamp().IsZero() {
		return claim, nil
	}
	old := parent.DeepCopyObject().(client.Object)
	annotations := parent.GetAnnotations()
	delete(annotations, annotation)
	parent.SetAnnotations(annotations)
	if err := cl.Patch(ctx, parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
		return "", fmt.Errorf("releasing inactive %s chain ownership: %w", s.policyWhat, err)
	}
	return "", nil
}

// policyOrder resolves Kubernetes references against this document, not
// Konnect status. A policy rejected by the builder cannot satisfy the chain.
func policyOrder[C chainObject, E any](
	ctx context.Context, c *configuration, cl client.Client, chain C, s policyChainScope[C, E],
) ([]E, string, error) {
	parent := chain.GetParentRef()
	parentKey, ok := policychain.ParentKey(chain)
	if !ok {
		return nil, "the chain must reference an " + chain.GetParentGVK().Kind, nil
	}
	refs := s.policies(chain)
	if refs == nil {
		return nil, "spec.apiSpec.policies must be supplied explicitly", nil
	}
	// The policies of another namespace need a KongReferenceGrant: without
	// it, the chain can't order them.
	message, err := siblingsNotGranted(ctx, cl, chain)
	if err != nil {
		return nil, "", err
	}
	if message != "" {
		return nil, message, nil
	}
	ordered := make([]E, 0, len(refs))
	read := make([]entityWithParent, 0, len(refs))
	seen := make(map[client.ObjectKey]struct{}, len(refs))
	for i, ref := range refs {
		path := fmt.Sprintf("spec.apiSpec.policies[%d]", i)
		if ref.name == "" || (ref.kind != "" && ref.kind != s.policyKind) {
			return nil, path + " must name an " + s.policyKind, nil
		}
		key := client.ObjectKey{Namespace: chain.GetNamespace(), Name: ref.name}
		if ref.namespace != "" {
			key.Namespace = ref.namespace
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, path + " duplicates policy " + key.String(), nil
		}
		seen[key] = struct{}{}
		policy := s.newPolicy()
		if err := cl.Get(ctx, key, policy); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, fmt.Sprintf("%s references %s %s, which does not exist", path, s.policyKind, key), nil
			}
			return nil, "", fmt.Errorf("reading %s %s: %w", s.policyWhat, key, err)
		}
		if commonv1alpha1.ObjectRefsDiffer(parent, policy.GetParentRef(), chain.GetNamespace(), policy.GetNamespace()) {
			return nil, fmt.Sprintf("%s references policy %s with a different %s", path, key, s.parentWhat), nil
		}
		if !policy.GetDeletionTimestamp().IsZero() {
			return nil, fmt.Sprintf("%s references policy %s, which is being deleted", path, key), nil
		}
		entity, included := c.built[policy.GetUID()].(E)
		if !included {
			return nil, fmt.Sprintf("%s references policy %s, which is not in the valid configuration", path, key), nil
		}
		ordered = append(ordered, entity)
		read = append(read, policy)
	}
	// The declared order must also be valid for the backend: a produce chain
	// can't order a child before its parent, nor interleave root groups.
	if s.validate != nil {
		if problem := s.validate(chain, refs, read); problem != "" {
			return nil, problem, nil
		}
	}

	// Every policy of the parent, of any namespace, in the configuration
	// must be listed: a policy of another namespace no KongReferenceGrant
	// lets reference the parent is left out of the configuration, and
	// needn't be.
	policies, err := s.listPolicies(ctx, cl, parentKey)
	if err != nil {
		return nil, "", fmt.Errorf("listing the complete %s set of %s: %w", s.policyWhat, parentKey, err)
	}
	for _, policy := range policies {
		if !policy.GetDeletionTimestamp().IsZero() {
			continue
		}
		key := client.ObjectKeyFromObject(policy)
		if _, declared := seen[key]; declared {
			continue
		}
		message, err := c.parentNotGranted(ctx, cl, policy, s.policyKind)
		if err != nil {
			return nil, "", err
		}
		if message == "" {
			return nil, fmt.Sprintf("spec.apiSpec.policies must list every %s exactly once; policy %s is missing", s.listWhat, policychain.NameIn(key, parentKey.Namespace)), nil
		}
	}
	return ordered, "", nil
}
