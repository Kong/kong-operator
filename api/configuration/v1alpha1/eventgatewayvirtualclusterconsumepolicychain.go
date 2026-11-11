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
	if parent.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || parent.NamespacedRef == nil ||
		parent.NamespacedRef.Name == "" ||
		(parent.NamespacedRef.Namespace != nil && *parent.NamespacedRef.Namespace != "" && *parent.NamespacedRef.Namespace != obj.Namespace) {
		return nil, PolicyChainInvalidError{Message: "spec.eventGatewayVirtualClusterRef must reference an EventGatewayVirtualCluster in the same namespace"}
	}

	var errs []error
	ids := make([]string, 0, len(obj.Spec.APISpec.Policies))
	policies := make(map[string]*EventGatewayVirtualClusterConsumePolicy, len(obj.Spec.APISpec.Policies))
	seenNames := make(map[string]struct{}, len(obj.Spec.APISpec.Policies))
	seenIDs := make(map[string]struct{}, len(obj.Spec.APISpec.Policies))
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
		if ns != obj.Namespace {
			errs = append(errs, commonv1alpha1.ReferenceCrossNamespaceError{Kind: kind, Namespace: ns, Name: ref.Name, ReferrerNamespace: obj.Namespace})
			continue
		}
		if _, duplicate := seenNames[ref.Name]; duplicate {
			errs = append(errs, PolicyChainInvalidError{Message: fmt.Sprintf("spec.apiSpec.policies[%d] duplicates policy %s/%s", i, ns, ref.Name)})
			continue
		}
		seenNames[ref.Name] = struct{}{}
		var policy EventGatewayVirtualClusterConsumePolicy
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
	currentRoot := ""
	for _, ref := range obj.Spec.APISpec.Policies {
		policy := policies[ref.Name]
		if err := validateConsumePolicyChainConfig(policy); err != nil {
			return nil, err
		}
		parentRef := policy.Spec.APISpec.ParentPolicyRef
		if parentRef.Name == "" {
			required := policy.Spec.APISpec.Type == EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordDecryptFieldsPolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordMaskFieldsConsumePolicyCreate ||
				policy.Spec.APISpec.Type == EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordTranscodeConsumePolicyCreate
			if parentRef.Kind != "" || parentRef.Namespace != "" || required {
				return nil, PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a valid parentPolicyRef", policy.Name)}
			}
			currentRoot = policy.Name
			continue
		}
		if err := validateConsumePolicyChainParent(policy, policies, currentRoot); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func validateConsumePolicyChainConfig(policy *EventGatewayVirtualClusterConsumePolicy) error {
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
	return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must have a supported apiSpec.type and exactly its configuration", policy.Name)}
}

func validateConsumePolicyChainParent(policy *EventGatewayVirtualClusterConsumePolicy, policies map[string]*EventGatewayVirtualClusterConsumePolicy, currentRoot string) error {
	ref := policy.Spec.APISpec.ParentPolicyRef
	if (ref.Kind != "" && ref.Kind != "EventGatewayVirtualClusterConsumePolicy") ||
		(ref.Namespace != "" && ref.Namespace != policy.Namespace) {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef must name an EventGatewayVirtualClusterConsumePolicy in the same namespace", policy.Name)}
	}
	if ref.Name == policy.Name {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef cannot reference itself", policy.Name)}
	}
	switch policy.Spec.APISpec.Type {
	case EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordDecryptFieldsPolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordMaskFieldsConsumePolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeParsedRecordTranscodeConsumePolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate,
		EventGatewayVirtualClusterConsumePolicyConfigTypeSkipRecordPolicyCreate:
	default:
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef is only supported for decryptFields, maskFields, transcode, modifyHeaders and skipRecord", policy.Name)}
	}
	parent, included := policies[ref.Name]
	if !included {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be included in spec.apiSpec.policies", policy.Name, ref.Name)}
	}
	if parent.Spec.APISpec.EventGatewayVirtualClusterConsumePolicyConfig == nil ||
		parent.Spec.APISpec.Type != EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy ||
		parent.Spec.APISpec.ConsumeSchemaValidationPolicy == nil ||
		parent.Spec.APISpec.ParentPolicyRef != (EventGatewayVirtualClusterConsumePolicyRef{}) {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s parentPolicyRef %s must be a root schemaValidation policy; deeper nesting is not supported", policy.Name, ref.Name)}
	}
	if currentRoot != parent.Name {
		return PolicyChainInvalidError{Message: fmt.Sprintf("policy %s must follow parent %s in its contiguous group; children cannot precede their parent or interleave other root groups", policy.Name, ref.Name)}
	}
	return nil
}
