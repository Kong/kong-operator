package ops

import (
	"context"
	"fmt"
	"slices"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

type resolvedPolicyChainIDsContextKey struct {
	kind       string
	key        client.ObjectKey
	uid        types.UID
	generation int64
}

type policyChainObject interface {
	client.Object
	GetTypeName() string
}

func policyChainIDsContextKey(obj policyChainObject) resolvedPolicyChainIDsContextKey {
	return resolvedPolicyChainIDsContextKey{
		kind: obj.GetTypeName(), key: client.ObjectKeyFromObject(obj), uid: obj.GetUID(), generation: obj.GetGeneration(),
	}
}

// WithResolvedPolicyChainIDs passes validated, ordered IDs to the chain operation
// in the same reconciliation. Callers must resolve them again for each reconcile.
func WithResolvedPolicyChainIDs(ctx context.Context, obj policyChainObject, ids []string) context.Context {
	if ids == nil {
		return ctx
	}
	return context.WithValue(ctx, policyChainIDsContextKey(obj), slices.Clone(ids))
}

func resolvedPolicyChainIDsFromContext(ctx context.Context, obj policyChainObject) ([]string, bool) {
	ids, ok := ctx.Value(policyChainIDsContextKey(obj)).([]string)
	return ids, ok
}

// applyEventGatewayVirtualClusterPolicyChain deliberately supports only reordering
// the complete remote set. Development backend probes confirmed this constraint;
// keep the guard rather than relying on all backend versions to enforce it.
func applyEventGatewayVirtualClusterPolicyChain(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.EventGatewayVirtualClusterPoliciesSDK,
	obj *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain,
	op Op,
) error {
	if obj.GetGatewayID() == "" {
		return CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Gateway", Op: op}
	}
	if obj.GetVirtualClusterID() == "" {
		return CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "EventGatewayVirtualCluster", Op: op}
	}
	desired, resolved := resolvedPolicyChainIDsFromContext(ctx, obj)
	if !resolved {
		var err error
		desired, err = obj.ResolveValidatedPolicyIDs(ctx, cl)
		if err != nil {
			return err
		}
	}
	req := sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest{
		GatewayID: obj.GetGatewayID(), VirtualClusterID: obj.GetVirtualClusterID(),
		UpdateEventGatewayPolicyChainRequest: &sdkkonnectcomp.UpdateEventGatewayPolicyChainRequest{Policies: desired},
	}
	remote, err := sdk.GetEventGatewayVirtualClusterClusterLevelPolicyChain(ctx, req.GatewayID, req.VirtualClusterID)
	if errWrap := wrapErrIfKonnectOpFailed(err, GetOp, obj); errWrap != nil {
		return errWrap
	}
	if remote == nil || remote.EventGatewayPolicyChainResponse == nil {
		return fmt.Errorf("getting cluster policy chain: %w", ErrNilResponse)
	}
	if err := validateCompletePolicyChain(desired, remote.EventGatewayPolicyChainResponse.Policies); err != nil {
		return err
	}
	resp, err := sdk.UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(ctx, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, op, obj); errWrap != nil {
		return errWrap
	}
	if resp == nil || resp.EventGatewayPolicyChainResponse == nil {
		return fmt.Errorf("applying cluster policy chain: %w", ErrNilResponse)
	}
	return validateAppliedPolicyChain(desired, resp.EventGatewayPolicyChainResponse.Policies, "cluster")
}

func validateCompletePolicyChain(desired, remote []string) error {
	return validateCompletePolicyChainForScope(desired, remote, "cluster")
}

func validateCompletePolicyChainForScope(desired, remote []string, scope string) error {
	if desired == nil || remote == nil {
		return configurationv1alpha1.PolicyChainInvalidError{Message: scope + " policy chain must contain an explicit policy list; an omitted or null list is not an empty chain"}
	}
	for _, ids := range [][]string{desired, remote} {
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if _, duplicate := seen[id]; duplicate || id == "" {
				return configurationv1alpha1.PolicyChainInvalidError{
					Message: scope + " policy chain must contain unique, non-empty policy IDs",
				}
			}
			seen[id] = struct{}{}
		}
	}
	sortedDesired, sortedRemote := slices.Clone(desired), slices.Clone(remote)
	slices.Sort(sortedDesired)
	slices.Sort(sortedRemote)
	if !slices.Equal(sortedDesired, sortedRemote) {
		return configurationv1alpha1.PolicyChainInvalidError{
			Message: "this controller only reorders the complete remote " + scope + " policy chain: spec.apiSpec.policies must reference every policy ID returned by GET exactly once; an empty list is allowed only when the remote chain is empty",
		}
	}
	return nil
}

func validateAppliedPolicyChain(desired, actual []string, scope string) error {
	if actual == nil || !slices.Equal(desired, actual) {
		return fmt.Errorf("%s policy chain response does not match the complete requested policy order", scope)
	}
	return nil
}

func policyChainOperationError(err error) error {
	if retryAfter, limited := GetRetryAfterFromRateLimitError(err); limited {
		return RateLimitError{Err: err, RetryAfter: retryAfter}
	}
	return err
}
