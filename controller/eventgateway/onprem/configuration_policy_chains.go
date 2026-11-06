package onprem

import (
	"context"
	"fmt"
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
	chains, parentIndex, _, err := childrenOf(ctx, c, cl, &list, &list.Items,
		index.IndexFieldEventGatewayVirtualClusterPolicyChainOnEventGatewayVirtualClusterRef, virtualClusters)
	if err != nil {
		return err
	}
	for _, excluded := range c.excluded[before:] {
		if excluded.obj.GetNamespace() == c.namespace {
			c.invalidChains = append(c.invalidChains, client.ObjectKeyFromObject(excluded.obj).String()+": "+excluded.message)
		}
	}
	for _, parent := range virtualClusters.keys {
		if parent.Namespace == c.namespace {
			if err := releaseInactiveClusterPolicyClaim(ctx, cl, parent); err != nil {
				return err
			}
		}
	}
	slices.SortFunc(chains, byCreation[*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain])
	for _, chain := range chains {
		status := entityStatus{obj: chain}
		parent := chain.Spec.EventGatewayVirtualClusterRef
		if chain.Namespace != c.namespace || parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef ||
			parent.NamespacedRef == nil || parent.NamespacedRef.Name == "" ||
			(parent.NamespacedRef.Namespace != nil && *parent.NamespacedRef.Namespace != "" && *parent.NamespacedRef.Namespace != chain.Namespace) {
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, "the chain must reference a virtual cluster in its own namespace and in the namespace of the OnPremEventGateway")
			continue
		}
		owner, err := policychain.Claim(ctx, cl, chain)
		if err != nil {
			return err
		}
		if owner != "" {
			message := fmt.Sprintf("EventGatewayVirtualCluster %s is owned by policy chain %s; delete that chain before claiming it", parent.NamespacedRef.Name, owner)
			status.conditions = []metav1.Condition{entityCondition(chain, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain", message)}
			c.exclude(status, eventgatewayv1alpha1.OnPremEntityInvalidReason, message)
			continue
		}
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
	return nil
}

// On-prem deletion is not a remote operation and needs no finalizer. A stale
// or terminating owner releases its claim even when no replacement exists.
func releaseInactiveClusterPolicyClaim(ctx context.Context, cl client.Client, key client.ObjectKey) error {
	var parent configurationv1alpha1.EventGatewayVirtualCluster
	if err := cl.Get(ctx, key, &parent); err != nil {
		return fmt.Errorf("reading virtual cluster for policy-chain ownership: %w", err)
	}
	claim := parent.Annotations[policychain.OwnerAnnotation]
	if claim == "" {
		return nil
	}
	name, uid, valid := strings.Cut(claim, "/")
	if valid && name != "" && uid != "" {
		var owner configurationv1alpha1.EventGatewayVirtualClusterPolicyChain
		err := cl.Get(ctx, client.ObjectKey{Namespace: key.Namespace, Name: name}, &owner)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("reading cluster policy-chain owner: %w", err)
		}
		if err == nil && string(owner.UID) == uid && owner.DeletionTimestamp.IsZero() {
			return nil
		}
	}
	old := parent.DeepCopy()
	delete(parent.Annotations, policychain.OwnerAnnotation)
	if err := cl.Patch(ctx, &parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("releasing inactive cluster policy-chain ownership: %w", err)
	}
	return nil
}

// clusterPolicyOrder resolves Kubernetes references against this document, not
// Konnect status. A policy rejected by the builder cannot satisfy the chain.
func (c *configuration) clusterPolicyOrder(
	ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain,
) ([]gwadminapi.EventGatewayClusterPolicyOnPrem, string, error) {
	if chain.Namespace != c.namespace {
		return nil, "the chain must be in the namespace of the OnPremEventGateway, " + c.namespace, nil
	}
	parent := chain.Spec.EventGatewayVirtualClusterRef
	ref := parent.NamespacedRef
	if parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref == nil || ref.Name == "" ||
		(ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != chain.Namespace) {
		return nil, "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster in the same namespace", nil
	}
	if chain.Spec.APISpec.Policies == nil {
		return nil, "spec.apiSpec.policies must be supplied explicitly", nil
	}
	ordered := make([]gwadminapi.EventGatewayClusterPolicyOnPrem, 0, len(chain.Spec.APISpec.Policies))
	seen := make(map[string]struct{}, len(chain.Spec.APISpec.Policies))
	for i, ref := range chain.Spec.APISpec.Policies {
		const kind = "EventGatewayVirtualClusterPolicy"
		path := fmt.Sprintf("spec.apiSpec.policies[%d]", i)
		if ref.Name == "" || (ref.Kind != "" && ref.Kind != kind) {
			return nil, path + " must name an " + kind, nil
		}
		if ref.Namespace != "" && ref.Namespace != chain.Namespace {
			return nil, path + " must reference a policy in the same namespace as the chain", nil
		}
		if _, duplicate := seen[ref.Name]; duplicate {
			return nil, path + " duplicates policy " + ref.Name, nil
		}
		seen[ref.Name] = struct{}{}
		var policy configurationv1alpha1.EventGatewayVirtualClusterPolicy
		key := client.ObjectKey{Namespace: chain.Namespace, Name: ref.Name}
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

	var policies configurationv1alpha1.EventGatewayVirtualClusterPolicyList
	parentKey := client.ObjectKey{Namespace: chain.Namespace, Name: ref.Name}
	if err := cl.List(ctx, &policies, client.InNamespace(chain.Namespace), client.MatchingFields{
		index.IndexFieldEventGatewayVirtualClusterPolicyOnEventGatewayVirtualClusterRef: parentKey.String(),
	}); err != nil {
		return nil, "", fmt.Errorf("listing the complete cluster policy set of %s: %w", parentKey, err)
	}
	for _, policy := range policies.Items {
		if policy.DeletionTimestamp.IsZero() {
			if _, declared := seen[policy.Name]; !declared {
				return nil, fmt.Sprintf("spec.apiSpec.policies must list every cluster-level policy exactly once; policy %s is missing", policy.Name), nil
			}
		}
	}
	return ordered, "", nil
}
