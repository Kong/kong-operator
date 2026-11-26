package v1alpha1

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// GetVirtualClusterRef returns the reference to the parent virtual cluster.
func (obj *EventGatewayVirtualClusterProducePolicyChain) GetVirtualClusterRef() commonv1alpha1.ObjectRef {
	return obj.GetEventGatewayVirtualClusterRef()
}

// ResolveValidatedPolicyIDs validates the complete root/child order using one
// snapshot per policy. Ordering never rewrites a policy's parentPolicyRef.
func (obj *EventGatewayVirtualClusterProducePolicyChain) ResolveValidatedPolicyIDs(ctx context.Context, cl client.Client) ([]string, error) {
	if obj.Spec.APISpec.Policies == nil {
		return nil, PolicyChainInvalidError{Message: "spec.apiSpec.policies must be supplied explicitly"}
	}
	parent := obj.Spec.EventGatewayVirtualClusterRef
	// The virtual cluster and the policies may be in other namespaces: the
	// KongReferenceGrants allowing it are checked before this is called.
	if parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || parent.NamespacedRef == nil ||
		parent.NamespacedRef.Name == "" {
		return nil, PolicyChainInvalidError{Message: "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster"}
	}

	var errs []error
	ids := make([]string, 0, len(obj.Spec.APISpec.Policies))
	policies := make(map[client.ObjectKey]*EventGatewayVirtualClusterProducePolicy, len(obj.Spec.APISpec.Policies))
	order := make([]client.ObjectKey, 0, len(obj.Spec.APISpec.Policies))
	seen := make(map[client.ObjectKey]struct{}, len(obj.Spec.APISpec.Policies))
	seenIDs := make(map[string]struct{}, len(obj.Spec.APISpec.Policies))
	for i, ref := range obj.Spec.APISpec.Policies {
		const kind = "EventGatewayVirtualClusterProducePolicy"
		if ref.Name == "" || (ref.Kind != "" && ref.Kind != kind) {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] must name an %s", i, kind)})
			continue
		}
		ns := ref.Namespace
		if ns == "" {
			ns = obj.Namespace
		}
		key := client.ObjectKey{Namespace: ns, Name: ref.Name}
		if _, duplicate := seen[key]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] duplicates policy %s/%s", i, ns, ref.Name)})
			continue
		}
		seen[key] = struct{}{}
		order = append(order, key)
		var policy EventGatewayVirtualClusterProducePolicy
		if err := cl.Get(ctx, key, &policy); err != nil {
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
		if _, duplicate := seenIDs[policy.GetKonnectID()]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("policy %s/%s duplicates Konnect policy ID %s", ns, ref.Name, policy.GetKonnectID())})
			continue
		}
		seenIDs[policy.GetKonnectID()] = struct{}{}
		policies[key] = &policy
		ids = append(ids, policy.GetKonnectID())
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if err := ValidateProducePolicyChainOrder(obj.Namespace, order, policies); err != nil {
		return nil, err
	}
	return ids, nil
}

// ValidateProducePolicyChainOrder checks the declared order of a complete
// produce policy set, of a chain of namespace: policies are the objects the
// keys of order name, all of them, of any namespace. A parent is a root
// schemaValidation policy, precedes its children, and each parent's group is
// contiguous. It is the order gw-admin accepts as well as Konnect, so both
// backends share it.
func ValidateProducePolicyChainOrder(namespace string, order []client.ObjectKey, policies map[client.ObjectKey]*EventGatewayVirtualClusterProducePolicy) error {
	var currentRoot client.ObjectKey
	for _, key := range order {
		policy := policies[key]
		if policy == nil {
			return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s is missing from the complete produce policy set", NameIn(key, namespace))}
		}
		if err := validateProducePolicyChainConfig(NameIn(key, namespace), policy); err != nil {
			return err
		}
		parentRef := policy.Spec.APISpec.ParentPolicyRef
		if parentRef.Name == "" {
			if parentRef.Kind != "" || parentRef.Namespace != "" ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate {
				return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a valid parentPolicyRef", NameIn(key, namespace))}
			}
			currentRoot = key
			continue
		}
		if err := validateProducePolicyChainParent(namespace, key, policy, policies, currentRoot); err != nil {
			return err
		}
	}
	return nil
}

func validateProducePolicyChainConfig(name string, policy *EventGatewayVirtualClusterProducePolicy) error {
	config := policy.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig
	if config != nil {
		switch config.Type {
		case EventGatewayVirtualClusterProducePolicyConfigTypeEncryptPolicy:
			if config.EncryptPolicy != nil {
				return nil
			}
		case EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate:
			if config.ParsedRecordEncryptFieldsPolicyCreate != nil {
				return nil
			}
		case EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate:
			if config.ParsedRecordMaskFieldsProducePolicyCreate != nil {
				return nil
			}
		case EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate:
			if config.ModifyHeadersPolicyCreate != nil {
				return nil
			}
		case EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy:
			if config.ProduceSchemaValidationPolicy != nil {
				return nil
			}
		case EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate:
			if config.ParsedRecordTranscodeProducePolicyCreate != nil {
				return nil
			}
		}
	}
	return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a supported apiSpec.type and its configuration", name)}
}

func validateProducePolicyChainParent(
	namespace string, key client.ObjectKey, policy *EventGatewayVirtualClusterProducePolicy,
	policies map[client.ObjectKey]*EventGatewayVirtualClusterProducePolicy, currentRoot client.ObjectKey,
) error {
	name := NameIn(key, namespace)
	ref := policy.Spec.APISpec.ParentPolicyRef
	// A parent of another namespace than the child's is allowed, with a
	// KongReferenceGrant the child's reconciliation checks: the chain only
	// requires it to be one of the policies it orders.
	parentKey := client.ObjectKey{Namespace: policy.Namespace, Name: ref.Name}
	if ref.Namespace != "" {
		parentKey.Namespace = ref.Namespace
	}
	parentName := NameIn(parentKey, namespace)
	if ref.Kind != "" && ref.Kind != "EventGatewayVirtualClusterProducePolicy" {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef must name an EventGatewayVirtualClusterProducePolicy", name)}
	}
	if parentKey == key {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef cannot reference itself", name)}
	}
	if policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate &&
		policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate &&
		policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate &&
		policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef is only supported for encryptFields, modifyHeaders, maskFields and transcode", name)}
	}
	parent, included := policies[parentKey]
	if !included {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be included in spec.apiSpec.policies", name, parentName)}
	}
	if parent.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig == nil ||
		parent.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy ||
		parent.Spec.APISpec.ProduceSchemaValidationPolicy == nil ||
		parent.Spec.APISpec.ParentPolicyRef != (EventGatewayVirtualClusterProducePolicyRef{}) {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be a root schemaValidation policy; deeper nesting is not supported", name, parentName)}
	}
	if currentRoot != parentKey {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must follow parent %s in its contiguous group; children cannot precede their parent or interleave other root groups", name, parentName)}
	}
	return nil
}
