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
	if parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || parent.NamespacedRef == nil ||
		parent.NamespacedRef.Name == "" ||
		(parent.NamespacedRef.Namespace != nil && *parent.NamespacedRef.Namespace != "" && *parent.NamespacedRef.Namespace != obj.Namespace) {
		return nil, PolicyChainInvalidError{Message: "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster in the same namespace"}
	}

	var errs []error
	ids := make([]string, 0, len(obj.Spec.APISpec.Policies))
	policies := make(map[string]*EventGatewayVirtualClusterProducePolicy, len(obj.Spec.APISpec.Policies))
	seenNames := make(map[string]struct{}, len(obj.Spec.APISpec.Policies))
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
		if ns != obj.Namespace {
			errs = append(errs, commonv1alpha1.ReferenceCrossNamespaceError{Kind: kind, Namespace: ns, Name: ref.Name, ReferrerNamespace: obj.Namespace})
			continue
		}
		if _, duplicate := seenNames[ref.Name]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] duplicates policy %s/%s", i, ns, ref.Name)})
			continue
		}
		seenNames[ref.Name] = struct{}{}
		var policy EventGatewayVirtualClusterProducePolicy
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
		if _, duplicate := seenIDs[policy.GetKonnectID()]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("policy %s/%s duplicates Konnect policy ID %s", ns, ref.Name, policy.GetKonnectID())})
			continue
		}
		seenIDs[policy.GetKonnectID()] = struct{}{}
		policies[ref.Name] = &policy
		ids = append(ids, policy.GetKonnectID())
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	order := make([]string, 0, len(obj.Spec.APISpec.Policies))
	for _, ref := range obj.Spec.APISpec.Policies {
		order = append(order, ref.Name)
	}
	if err := ValidateProducePolicyChainOrder(order, policies); err != nil {
		return nil, err
	}
	return ids, nil
}

// ValidateProducePolicyChainOrder checks the declared order of a complete
// produce policy set: policies are the objects the names of order name, all
// of them. A parent is a root schemaValidation policy, precedes its children,
// and each parent's group is contiguous. It is the order gw-admin accepts as
// well as Konnect, so both backends share it.
func ValidateProducePolicyChainOrder(order []string, policies map[string]*EventGatewayVirtualClusterProducePolicy) error {
	currentRoot := ""
	for _, name := range order {
		policy := policies[name]
		if err := validateProducePolicyChainConfig(policy); err != nil {
			return err
		}
		parentRef := policy.Spec.APISpec.ParentPolicyRef
		if parentRef.Name == "" {
			if parentRef.Kind != "" || parentRef.Namespace != "" ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate {
				return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a valid parentPolicyRef", policy.Name)}
			}
			currentRoot = policy.Name
			continue
		}
		if err := validateProducePolicyChainParent(policy, policies, currentRoot); err != nil {
			return err
		}
	}
	return nil
}

func validateProducePolicyChainConfig(policy *EventGatewayVirtualClusterProducePolicy) error {
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
	return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a supported apiSpec.type and its configuration", policy.Name)}
}

func validateProducePolicyChainParent(policy *EventGatewayVirtualClusterProducePolicy, policies map[string]*EventGatewayVirtualClusterProducePolicy, currentRoot string) error {
	ref := policy.Spec.APISpec.ParentPolicyRef
	if (ref.Kind != "" && ref.Kind != "EventGatewayVirtualClusterProducePolicy") ||
		(ref.Namespace != "" && ref.Namespace != policy.Namespace) {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef must name an EventGatewayVirtualClusterProducePolicy in the same namespace", policy.Name)}
	}
	if ref.Name == policy.Name {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef cannot reference itself", policy.Name)}
	}
	if policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate &&
		policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate &&
		policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate &&
		policy.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef is only supported for encryptFields, modifyHeaders, maskFields and transcode", policy.Name)}
	}
	parent, included := policies[ref.Name]
	if !included {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be included in spec.apiSpec.policies", policy.Name, ref.Name)}
	}
	if parent.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig == nil ||
		parent.Spec.APISpec.Type != EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy ||
		parent.Spec.APISpec.ProduceSchemaValidationPolicy == nil ||
		parent.Spec.APISpec.ParentPolicyRef != (EventGatewayVirtualClusterProducePolicyRef{}) {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be a root schemaValidation policy; deeper nesting is not supported", policy.Name, ref.Name)}
	}
	if currentRoot != parent.Name {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must follow parent %s in its contiguous group; children cannot precede their parent or interleave other root groups", policy.Name, ref.Name)}
	}
	return nil
}
