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
		parent.NamespacedRef.Name == "" ||
		(parent.NamespacedRef.Namespace != nil && *parent.NamespacedRef.Namespace != "" && *parent.NamespacedRef.Namespace != obj.Namespace) {
		return nil, PolicyChainInvalidError{Message: "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster in the same namespace"}
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
		if ns != obj.Namespace {
			errs = append(errs, commonv1alpha1.ReferenceCrossNamespaceError{Kind: kind, Namespace: ns, Name: ref.Name, ReferrerNamespace: obj.Namespace})
			continue
		}
		if _, duplicate := seen[ref.Name]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] duplicates policy %s/%s", i, ns, ref.Name)})
			continue
		}
		seen[ref.Name] = struct{}{}
		var policy EventGatewayVirtualClusterPolicy
		if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, &policy); err != nil {
			if apierrors.IsNotFound(err) {
				errs = append(errs, commonv1alpha1.ReferenceNotFoundError{Kind: kind, Namespace: ns, Name: ref.Name, Err: err})
			} else {
				errs = append(errs, fmt.Errorf("getting policy %s/%s: %w", ns, ref.Name, err))
			}
			continue
		}
		if commonv1alpha1.ObjectRefsDiffer(parent, policy.Spec.EventGatewayVirtualClusterRef, obj.Namespace, policy.Namespace) ||
			(obj.GetVirtualClusterID() != "" && policy.GetVirtualClusterID() != obj.GetVirtualClusterID()) {
			errs = append(errs, commonv1alpha1.ReferenceDifferentParentError{Kind: kind, Namespace: ns, Name: ref.Name, ParentKind: "EventGatewayVirtualCluster"})
			continue
		}
		if obj.GetGatewayID() != "" && policy.GetGatewayID() != obj.GetGatewayID() {
			errs = append(errs, commonv1alpha1.ReferenceDifferentGatewayError{
				Kind: kind, Namespace: ns, Name: ref.Name,
				ReferrerGatewayID: obj.GetGatewayID(), ReferencedGatewayID: policy.GetGatewayID(),
			})
			continue
		}
		if !policy.DeletionTimestamp.IsZero() {
			errs = append(errs, commonv1alpha1.ReferenceBeingDeletedError{Kind: kind, Namespace: ns, Name: ref.Name})
			continue
		}
		programmed := apimeta.FindStatusCondition(policy.Status.Conditions, "Programmed")
		if policy.GetKonnectID() == "" || programmed == nil || programmed.Status != metav1.ConditionTrue ||
			programmed.ObservedGeneration != policy.Generation {
			errs = append(errs, commonv1alpha1.ReferenceNotProgrammedError{Kind: kind, Namespace: ns, Name: ref.Name})
			continue
		}
		ids = append(ids, policy.GetKonnectID())
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return ids, nil
}
