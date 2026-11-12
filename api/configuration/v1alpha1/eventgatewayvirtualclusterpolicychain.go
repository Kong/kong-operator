package v1alpha1

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// PolicyChainInvalidError identifies an invalid policy-chain declaration.
//
// +kubebuilder:object:generate=false
type PolicyChainInvalidError struct {
	Message string
}

func (e PolicyChainInvalidError) Error() string { return e.Message }

// ValidatePolicyReferences checks the chain before resolving its ordered IDs.
// A persisted ID alone does not mean a policy's current generation is ready.
func (obj *EventGatewayVirtualClusterPolicyChain) ValidatePolicyReferences(ctx context.Context, cl client.Client) error {
	_, err := obj.ResolveValidatedPolicyIDs(ctx, cl)
	return err
}

// ResolveValidatedPolicyIDs validates policy references and resolves IDs from the
// same snapshots, preserving their declared order.
func (obj *EventGatewayVirtualClusterPolicyChain) ResolveValidatedPolicyIDs(ctx context.Context, cl client.Client) ([]string, error) {
	return resolveChainPolicyIDs(ctx, cl, obj.Namespace, obj.Spec.EventGatewayVirtualClusterRef, "spec.eventGatewayVirtualClusterRef",
		"EventGatewayVirtualCluster", obj.GetVirtualClusterID(), "EventGatewayVirtualClusterPolicy", obj.GetGatewayID(),
		chainPolicyRefs(obj.Spec.APISpec.Policies, func(r EventGatewayVirtualClusterPolicyRef) chainPolicyRef { return chainPolicyRef(r) }),
		func(p *EventGatewayVirtualClusterPolicy) string { return p.GetVirtualClusterID() })
}

// chainPolicyRef is a reference of a policy chain to a policy it orders.
//
// +kubebuilder:object:generate=false
type chainPolicyRef struct {
	Kind, Name, Namespace string
}

// chainPolicyRefs returns the references of a chain to its policies, nil
// when it doesn't supply them.
func chainPolicyRefs[R any](refs []R, of func(R) chainPolicyRef) []chainPolicyRef {
	if refs == nil {
		return nil
	}
	out := make([]chainPolicyRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, of(r))
	}
	return out
}

// chainPolicy is a policy a chain orders.
type chainPolicy interface {
	k8sutils.ConditionsAwareObject
	GetParentRef() commonv1alpha1.ObjectRef
	GetGatewayID() string
	GetKonnectID() string
}

// resolveChainPolicyIDs validates the references refs, of a chain of
// namespace, to the policies of policyKind it orders, which must all be
// attached to its parent, referenced by parentField, of parentKind and
// Konnect ID parentID, on the gateway of gatewayID; policyParentID returns
// the parent ID of a policy. It resolves their Konnect IDs from the same
// snapshots, in their declared order. Any invalid reference rejects the
// whole result.
func resolveChainPolicyIDs[P any, PP interface {
	*P
	chainPolicy
}](
	ctx context.Context, cl client.Client, namespace string,
	parent commonv1alpha1.ObjectRef, parentField, parentKind, parentID, policyKind, gatewayID string,
	refs []chainPolicyRef, policyParentID func(PP) string,
) ([]string, error) {
	if refs == nil {
		return nil, PolicyChainInvalidError{Message: "spec.apiSpec.policies must be supplied explicitly"}
	}
	if parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || parent.NamespacedRef == nil ||
		parent.NamespacedRef.Name == "" {
		return nil, PolicyChainInvalidError{Message: parentField + " must reference an " + parentKind}
	}

	var errs []error
	ids := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for i, ref := range refs {
		if ref.Name == "" || (ref.Kind != "" && ref.Kind != policyKind) {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] must name an %s", i, policyKind)})
			continue
		}
		ns := ref.Namespace
		if ns == "" {
			ns = namespace
		}
		key := ns + "/" + ref.Name
		if _, duplicate := seen[key]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] duplicates policy %s/%s", i, ns, ref.Name)})
			continue
		}
		seen[key] = struct{}{}
		policy := PP(new(P))
		if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, policy); err != nil {
			if apierrors.IsNotFound(err) {
				errs = append(errs, commonv1alpha1.ReferenceNotFoundError{Kind: policyKind, Namespace: ns, Name: ref.Name, Err: err})
			} else {
				errs = append(errs, fmt.Errorf("getting policy %s/%s: %w", ns, ref.Name, err))
			}
			continue
		}
		if err := validatePolicyChainPolicy(policy, policyKind, parent, namespace,
			parentKind, parentID, policyParentID(policy), gatewayID); err != nil {
			errs = append(errs, err)
			continue
		}
		ids = append(ids, policy.GetKonnectID())
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return ids, nil
}

func validatePolicyChainPolicy(
	policy chainPolicy,
	kind string,
	parent commonv1alpha1.ObjectRef,
	namespace, parentKind, parentID, policyParentID, gatewayID string,
) error {
	ns, name := policy.GetNamespace(), policy.GetName()
	// A policy not programmed yet has no gateway nor parent ID: it is
	// reported as not programmed below, not as of another one.
	if commonv1alpha1.ObjectRefsDiffer(parent, policy.GetParentRef(), namespace, ns) ||
		(parentID != "" && policyParentID != "" && policyParentID != parentID) {
		return commonv1alpha1.ReferenceDifferentParentError{Kind: kind, Namespace: ns, Name: name, ParentKind: parentKind}
	}
	if gatewayID != "" && policy.GetGatewayID() != "" && policy.GetGatewayID() != gatewayID {
		return commonv1alpha1.ReferenceDifferentGatewayError{
			Kind: kind, Namespace: ns, Name: name,
			ReferrerGatewayID: gatewayID, ReferencedGatewayID: policy.GetGatewayID(),
		}
	}
	if !policy.GetDeletionTimestamp().IsZero() {
		return commonv1alpha1.ReferenceBeingDeletedError{Kind: kind, Namespace: ns, Name: name}
	}
	programmed := apimeta.FindStatusCondition(policy.GetConditions(), "Programmed")
	if policy.GetKonnectID() == "" || programmed == nil || programmed.Status != metav1.ConditionTrue ||
		programmed.ObservedGeneration != policy.GetGeneration() {
		return commonv1alpha1.ReferenceNotProgrammedError{Kind: kind, Namespace: ns, Name: name}
	}
	return nil
}
