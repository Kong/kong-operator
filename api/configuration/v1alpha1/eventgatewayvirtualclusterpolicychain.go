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
	if obj.Spec.APISpec.Policies == nil {
		return nil, PolicyChainInvalidError{Message: "spec.apiSpec.policies must be supplied explicitly"}
	}
	parent := obj.Spec.EventGatewayVirtualClusterRef
	if parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || parent.NamespacedRef == nil ||
		parent.NamespacedRef.Name == "" {
		return nil, PolicyChainInvalidError{Message: "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster"}
	}

	var errs []error
	ids := make([]string, 0, len(obj.Spec.APISpec.Policies))
	seen := make(map[string]struct{}, len(obj.Spec.APISpec.Policies))
	for i, ref := range obj.Spec.APISpec.Policies {
		const kind = "EventGatewayVirtualClusterPolicy"
		if ref.Name == "" || (ref.Kind != "" && ref.Kind != kind) {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] must name an %s", i, kind)})
			continue
		}
		ns := ref.Namespace
		if ns == "" {
			ns = obj.Namespace
		}
		key := ns + "/" + ref.Name
		if _, duplicate := seen[key]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] duplicates policy %s/%s", i, ns, ref.Name)})
			continue
		}
		seen[key] = struct{}{}
		var policy EventGatewayVirtualClusterPolicy
		if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, &policy); err != nil {
			if apierrors.IsNotFound(err) {
				errs = append(errs, commonv1alpha1.ReferenceNotFoundError{Kind: kind, Namespace: ns, Name: ref.Name, Err: err})
			} else {
				errs = append(errs, fmt.Errorf("getting policy %s/%s: %w", ns, ref.Name, err))
			}
			continue
		}
		if err := validatePolicyChainPolicy(&policy, kind, parent, obj.Namespace,
			"EventGatewayVirtualCluster", obj.GetVirtualClusterID(), policy.GetVirtualClusterID(), obj.GetGatewayID()); err != nil {
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
	policy interface {
		k8sutils.ConditionsAwareObject
		GetParentRef() commonv1alpha1.ObjectRef
		GetGatewayID() string
		GetKonnectID() string
	},
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
