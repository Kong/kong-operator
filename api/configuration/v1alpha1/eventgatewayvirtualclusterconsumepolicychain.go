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
func (obj *EventGatewayVirtualClusterConsumePolicyChain) GetVirtualClusterRef() commonv1alpha1.ObjectRef {
	return obj.GetEventGatewayVirtualClusterRef()
}

// ResolveValidatedPolicyIDs validates the complete root/child order using one
// snapshot per policy. Ordering never rewrites a policy's parentPolicyRef.
func (obj *EventGatewayVirtualClusterConsumePolicyChain) ResolveValidatedPolicyIDs(ctx context.Context, cl client.Client) ([]string, error) {
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
	policies := make(map[client.ObjectKey]*EventGatewayVirtualClusterConsumePolicy, len(obj.Spec.APISpec.Policies))
	order := make([]client.ObjectKey, 0, len(obj.Spec.APISpec.Policies))
	seen := make(map[client.ObjectKey]struct{}, len(obj.Spec.APISpec.Policies))
	seenIDs := make(map[string]struct{}, len(obj.Spec.APISpec.Policies))
	organizationID := obj.GetKonnectStatus().GetOrgID()
	for i, ref := range obj.Spec.APISpec.Policies {
		const kind = "EventGatewayVirtualClusterConsumePolicy"
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
		var policy EventGatewayVirtualClusterConsumePolicy
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
		policyOrganizationID := policy.GetKonnectStatus().GetOrgID()
		if organizationID != "" && policyOrganizationID != "" && policyOrganizationID != organizationID {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("policy %s/%s belongs to Konnect organization %s, expected organization %s", ns, ref.Name, policyOrganizationID, organizationID)})
			continue
		}
		if organizationID == "" {
			organizationID = policyOrganizationID
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
	if err := ValidateConsumePolicyChainOrder(obj.Namespace, order, policies); err != nil {
		return nil, err
	}
	return ids, nil
}

// ValidateConsumePolicyChainOrder checks the complete root/child order shared
// by Konnect and the on-prem document, without consulting backend status, of
// a chain of namespace: policies are the objects the keys of order name, all
// of them, of any namespace. Each root schemaValidation group is contiguous
// and precedes its children.
func ValidateConsumePolicyChainOrder(namespace string, order []client.ObjectKey, policies map[client.ObjectKey]*EventGatewayVirtualClusterConsumePolicy) error {
	var currentRoot client.ObjectKey
	for _, key := range order {
		policy := policies[key]
		if policy == nil {
			return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s is missing from the complete consume policy set", policyNameIn(key, namespace))}
		}
		if err := validateConsumePolicyChainConfig(policyNameIn(key, namespace), policy); err != nil {
			return err
		}
		parentRef := policy.Spec.APISpec.ParentPolicyRef
		if parentRef.Name == "" {
			required := policy.Spec.APISpec.Type == EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordDecryptFieldsPolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordMaskFieldsConsumePolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordTranscodeConsumePolicyCreate
			if parentRef.Kind != "" || parentRef.Namespace != "" || required {
				return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a valid parentPolicyRef", policyNameIn(key, namespace))}
			}
			currentRoot = key
			continue
		}
		if err := validateConsumePolicyChainParent(namespace, key, policy, policies, currentRoot); err != nil {
			return err
		}
	}
	return nil
}

func validateConsumePolicyChainConfig(name string, policy *EventGatewayVirtualClusterConsumePolicy) error {
	config := policy.Spec.APISpec.EventGatewayVirtualClusterConsumePolicyConfig
	if config != nil {
		members := 0
		for _, present := range []bool{
			config.DecryptPolicy != nil, config.ParsedRecordDecryptFieldsPolicyCreate != nil,
			config.ParsedRecordMaskFieldsConsumePolicyCreate != nil, config.ModifyHeadersPolicyCreate != nil,
			config.ConsumeSchemaValidationPolicy != nil, config.SkipRecordPolicyCreate != nil,
			config.ParsedRecordTranscodeConsumePolicyCreate != nil,
		} {
			if present {
				members++
			}
		}
		if members == 1 {
			switch config.Type {
			case EventGatewayVirtualClusterConsumePolicyConfigTypeDecryptPolicy:
				if config.DecryptPolicy != nil {
					return nil
				}
			case EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordDecryptFieldsPolicyCreate:
				if config.ParsedRecordDecryptFieldsPolicyCreate != nil {
					return nil
				}
			case EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordMaskFieldsConsumePolicyCreate:
				if config.ParsedRecordMaskFieldsConsumePolicyCreate != nil {
					return nil
				}
			case EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate:
				if config.ModifyHeadersPolicyCreate != nil {
					return nil
				}
			case EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy:
				if config.ConsumeSchemaValidationPolicy != nil {
					return nil
				}
			case EventGatewayVirtualClusterConsumePolicyConfigTypeSkipRecordPolicyCreate:
				if config.SkipRecordPolicyCreate != nil {
					return nil
				}
			case EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordTranscodeConsumePolicyCreate:
				if config.ParsedRecordTranscodeConsumePolicyCreate != nil {
					return nil
				}
			}
		}
	}
	return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a supported apiSpec.type and exactly its configuration", name)}
}

func validateConsumePolicyChainParent(
	namespace string, key client.ObjectKey, policy *EventGatewayVirtualClusterConsumePolicy,
	policies map[client.ObjectKey]*EventGatewayVirtualClusterConsumePolicy, currentRoot client.ObjectKey,
) error {
	name := policyNameIn(key, namespace)
	ref := policy.Spec.APISpec.ParentPolicyRef
	// A parent of another namespace than the child's is allowed, with a
	// KongReferenceGrant the child's reconciliation checks: the chain only
	// requires it to be one of the policies it orders.
	parentKey := client.ObjectKey{Namespace: policy.Namespace, Name: ref.Name}
	if ref.Namespace != "" {
		parentKey.Namespace = ref.Namespace
	}
	parentName := policyNameIn(parentKey, namespace)
	if ref.Kind != "" && ref.Kind != "EventGatewayVirtualClusterConsumePolicy" {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef must name an EventGatewayVirtualClusterConsumePolicy", name)}
	}
	if parentKey == key {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef cannot reference itself", name)}
	}
	switch policy.Spec.APISpec.Type {
	case EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordDecryptFieldsPolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordMaskFieldsConsumePolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordTranscodeConsumePolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeSkipRecordPolicyCreate:
	default:
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef is only supported for decryptFields, maskFields, transcode, modifyHeaders and skipRecord", name)}
	}
	parent, included := policies[parentKey]
	if !included {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be included in spec.apiSpec.policies", name, parentName)}
	}
	if parent.Spec.APISpec.EventGatewayVirtualClusterConsumePolicyConfig == nil ||
		parent.Spec.APISpec.Type != EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy ||
		parent.Spec.APISpec.ConsumeSchemaValidationPolicy == nil ||
		parent.Spec.APISpec.ParentPolicyRef != (EventGatewayVirtualClusterConsumePolicyRef{}) {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be a root schemaValidation policy; deeper nesting is not supported", name, parentName)}
	}
	if currentRoot != parentKey {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must follow parent %s in its contiguous group; children cannot precede their parent or interleave other root groups", name, parentName)}
	}
	return nil
}
