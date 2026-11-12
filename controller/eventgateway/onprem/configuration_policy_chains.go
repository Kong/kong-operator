package onprem

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	return "Cluster policy chains prevent applying the complete gateway configuration: " + strings.Join(e.problems, "; ")
}

func (c *configuration) addClusterPolicyChains(ctx context.Context, cl client.Client, virtualClusters parents) error {
	var list configurationv1alpha1.EventGatewayVirtualClusterPolicyChainList
	before := len(c.excluded)
	// A chain left out doesn't hold back the configuration by itself, e.g. one
	// competing for a virtual cluster another chain owns: invalidChains, of
	// the chains owning their virtual cluster, does.
	defer func() {
		for i := before; i < len(c.excluded); i++ {
			c.excluded[i].blocks = false
		}
	}()
	chains, parentIndex, _, err := childrenOf(ctx, c, cl, &list, &list.Items,
		index.IndexFieldEventGatewayVirtualClusterPolicyChainOnEventGatewayVirtualClusterRef, virtualClusters)
	if err != nil {
		return err
	}
	activeClaims := make(map[client.ObjectKey]string, len(virtualClusters.keys))
	for _, parent := range virtualClusters.keys {
		// A virtual cluster of another namespace no KongReferenceGrant lets
		// reference its backend cluster doesn't belong to the gateway: neither
		// it nor its chains hold back the configuration, and its claim isn't
		// the gateway's to release.
		if c.leftOutNotPermitted(reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualCluster](), parent) {
			continue
		}
		claim, err := releaseInactiveClusterPolicyClaim(ctx, cl, parent)
		if err != nil {
			return err
		}
		activeClaims[parent] = claim
	}
	for _, excluded := range c.excluded[before:] {
		chain := excluded.obj.(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain)
		parent, ok := policychain.ParentKey(chain)
		// A chain of another namespace no KongReferenceGrant lets reference its
		// virtual cluster doesn't belong to the gateway: owning it, it keeps
		// the default order of its policies, as a chain whose claim is taken.
		if !ok || excluded.reason == eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason {
			continue
		}
		if claim, ok := activeClaims[parent]; ok && claim == policychain.ClaimValue(chain) {
			c.invalidChains = append(c.invalidChains, client.ObjectKeyFromObject(chain).String()+": "+excluded.message)
		}
	}
	slices.SortFunc(chains, byCreation[*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain])
	// owned are the virtual clusters a chain owns; notPermitted, the chains
	// no KongReferenceGrant lets order their policies, by virtual cluster.
	owned := map[client.ObjectKey]bool{}
	notPermitted := map[client.ObjectKey][]string{}
	for _, chain := range chains {
		status := entityStatus{obj: chain}
		parent, ok := policychain.ParentKey(chain)
		if !ok {
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, "the chain must reference an EventGatewayVirtualCluster")
			continue
		}
		// Policies of another namespace no KongReferenceGrant lets it
		// reference: reported as such, like for any other entity. As against
		// Konnect, such a chain doesn't claim its virtual cluster.
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
			message := fmt.Sprintf("EventGatewayVirtualCluster %s is owned by policy chain %s; delete that chain before claiming it", policychain.NameIn(parent, chain.Namespace), owner)
			status.conditions = []metav1.Condition{entityCondition(chain, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain", message)}
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
			continue
		}
		// Only the chain holding the claim owns the virtual cluster: one that
		// lost it to a chain that can't order its policies doesn't release
		// the push.
		owned[parent] = true
		status.conditions = []metav1.Condition{entityCondition(chain, "PolicyChainOwnerValid", metav1.ConditionTrue, "Owner", "This resource owns the parent cluster policy chain")}
		ordered, problem, err := c.clusterPolicyOrder(ctx, cl, chain)
		if err != nil {
			return err
		}
		i := parentIndex[client.ObjectKeyFromObject(chain)]
		vc := &c.entities.VirtualClusters[i]
		// Validation errors returned by gw-admin refer to array positions.
		// Move their source-object paths along with the reordered entities.
		path := fmt.Sprintf("virtual_clusters.%d.cluster_policies", i)
		sources := make(map[gwadminapi.UUIDv4]string, len(vc.ClusterPolicies))
		for j, p := range vc.ClusterPolicies {
			sources[p.Id] = c.objects[fmt.Sprintf("%s.%d", path, j)]
		}
		if problem == "" && (len(ordered) != len(vc.ClusterPolicies) || slices.ContainsFunc(ordered, func(p gwadminapi.EventGatewayClusterPolicyOnPrem) bool {
			_, included := sources[p.Id]
			return !included
		})) {
			problem = "the cluster policy set changed during configuration assembly; waiting for a consistent configuration"
		}
		if problem != "" {
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, problem)
			c.invalidChains = append(c.invalidChains, client.ObjectKeyFromObject(chain).String()+": "+problem)
			continue
		}
		for j, p := range ordered {
			c.objects[fmt.Sprintf("%s.%d", path, j)] = sources[p.Id]
		}
		vc.ClusterPolicies = ordered
		c.orderedClusterPolicies = append(c.orderedClusterPolicies, vc.Id)
		c.included = append(c.included, status)
	}
	// A chain that can't order its policies holds back the configuration of a
	// virtual cluster no other chain owns: it would keep the default order of
	// its policies.
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

// On-prem deletion is not a remote operation and needs no finalizer. A stale
// or terminating owner releases its claim even when no replacement exists.
// The surviving UID-bound claim is returned without acquiring a new one.
func releaseInactiveClusterPolicyClaim(ctx context.Context, cl client.Client, key client.ObjectKey) (string, error) {
	var parent configurationv1alpha1.EventGatewayVirtualCluster
	if err := cl.Get(ctx, key, &parent); err != nil {
		return "", fmt.Errorf("reading virtual cluster for policy-chain ownership: %w", err)
	}
	claim := parent.Annotations[policychain.OwnerAnnotation]
	if claim == "" {
		return "", nil
	}
	owner, err := policychain.Holder(ctx, cl, &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{}, key, claim)
	if err != nil {
		return "", err
	}
	if owner != nil && owner.GetDeletionTimestamp().IsZero() {
		return claim, nil
	}
	old := parent.DeepCopy()
	delete(parent.Annotations, policychain.OwnerAnnotation)
	if err := cl.Patch(ctx, &parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
		return "", fmt.Errorf("releasing inactive cluster policy-chain ownership: %w", err)
	}
	return "", nil
}

// clusterPolicyOrder resolves Kubernetes references against this document, not
// Konnect status. A policy rejected by the builder cannot satisfy the chain.
func (c *configuration) clusterPolicyOrder(
	ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain,
) ([]gwadminapi.EventGatewayClusterPolicyOnPrem, string, error) {
	parent := chain.Spec.EventGatewayVirtualClusterRef
	parentKey, ok := policychain.ParentKey(chain)
	if !ok {
		return nil, "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster", nil
	}
	if chain.Spec.APISpec.Policies == nil {
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
	ordered := make([]gwadminapi.EventGatewayClusterPolicyOnPrem, 0, len(chain.Spec.APISpec.Policies))
	seen := make(map[client.ObjectKey]struct{}, len(chain.Spec.APISpec.Policies))
	for i, ref := range chain.Spec.APISpec.Policies {
		const kind = "EventGatewayVirtualClusterPolicy"
		path := fmt.Sprintf("spec.apiSpec.policies[%d]", i)
		if ref.Name == "" || (ref.Kind != "" && ref.Kind != kind) {
			return nil, path + " must name an " + kind, nil
		}
		key := client.ObjectKey{Namespace: chain.Namespace, Name: ref.Name}
		if ref.Namespace != "" {
			key.Namespace = ref.Namespace
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, path + " duplicates policy " + key.String(), nil
		}
		seen[key] = struct{}{}
		var policy configurationv1alpha1.EventGatewayVirtualClusterPolicy
		if err := cl.Get(ctx, key, &policy); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, fmt.Sprintf("%s references %s %s, which does not exist", path, kind, key), nil
			}
			return nil, "", fmt.Errorf("reading cluster policy %s: %w", key, err)
		}
		if commonv1alpha1.ObjectRefsDiffer(parent, policy.Spec.EventGatewayVirtualClusterRef, chain.Namespace, policy.Namespace) {
			return nil, fmt.Sprintf("%s references policy %s with a different virtual cluster", path, key), nil
		}
		if !policy.DeletionTimestamp.IsZero() {
			return nil, fmt.Sprintf("%s references policy %s, which is being deleted", path, key), nil
		}
		entity, included := c.built[policy.UID].(gwadminapi.EventGatewayClusterPolicyOnPrem)
		if !included {
			return nil, fmt.Sprintf("%s references policy %s, which is not in the valid configuration", path, key), nil
		}
		ordered = append(ordered, entity)
	}

	// Every policy of the virtual cluster, of any namespace, in the
	// configuration must be listed: a policy of another namespace no
	// KongReferenceGrant lets reference the virtual cluster is left out of
	// the configuration, and needn't be.
	var policies configurationv1alpha1.EventGatewayVirtualClusterPolicyList
	if err := cl.List(ctx, &policies, client.MatchingFields{
		index.IndexFieldEventGatewayVirtualClusterPolicyOnEventGatewayVirtualClusterRef: parentKey.String(),
	}); err != nil {
		return nil, "", fmt.Errorf("listing the complete cluster policy set of %s: %w", parentKey, err)
	}
	for _, policy := range policies.Items {
		if !policy.DeletionTimestamp.IsZero() {
			continue
		}
		key := client.ObjectKeyFromObject(&policy)
		if _, declared := seen[key]; declared {
			continue
		}
		message, err := c.parentNotGranted(ctx, cl, &policy, "EventGatewayVirtualClusterPolicy")
		if err != nil {
			return nil, "", err
		}
		if message == "" {
			return nil, fmt.Sprintf("spec.apiSpec.policies must list every cluster-level policy exactly once; policy %s is missing", policychain.NameIn(key, parentKey.Namespace)), nil
		}
	}
	return ordered, "", nil
}
