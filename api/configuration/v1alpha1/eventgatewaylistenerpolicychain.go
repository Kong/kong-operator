package v1alpha1

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// ResolveValidatedPolicyIDs resolves the complete declared order from one snapshot
// of each policy. Any invalid reference rejects the entire result.
func (obj *EventGatewayListenerPolicyChain) ResolveValidatedPolicyIDs(ctx context.Context, cl client.Client) ([]string, error) {
	if obj.Spec.APISpec.Policies == nil {
		return nil, PolicyChainInvalidError{Message: "spec.apiSpec.policies must be supplied explicitly"}
	}
	parent := obj.Spec.EventGatewayListenerRef
	if parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || parent.NamespacedRef == nil ||
		parent.NamespacedRef.Name == "" ||
		(parent.NamespacedRef.Namespace != nil && *parent.NamespacedRef.Namespace != "" && *parent.NamespacedRef.Namespace != obj.Namespace) {
		return nil, PolicyChainInvalidError{Message: "spec.eventGatewayListenerRef must reference an EventGatewayListener in the same namespace"}
	}

	var errs []error
	ids := make([]string, 0, len(obj.Spec.APISpec.Policies))
	seen := make(map[string]struct{}, len(obj.Spec.APISpec.Policies))
	for i, ref := range obj.Spec.APISpec.Policies {
		const kind = "EventGatewayListenerPolicy"
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
		var policy EventGatewayListenerPolicy
		if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, &policy); err != nil {
			if apierrors.IsNotFound(err) {
				errs = append(errs, commonv1alpha1.ReferenceNotFoundError{Kind: kind, Namespace: ns, Name: ref.Name, Err: err})
			} else {
				errs = append(errs, fmt.Errorf("getting policy %s/%s: %w", ns, ref.Name, err))
			}
			continue
		}
		if err := validatePolicyChainPolicy(&policy, kind, parent, obj.Namespace,
			"EventGatewayListener", obj.GetEventGatewayListenerID(), policy.GetEventGatewayListenerID(), obj.GetGatewayID()); err != nil {
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
