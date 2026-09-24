package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// AIGatewayRefGroup is the API group of the AI Gateway (control plane) kinds
// that an AIGatewayRef can reference.
//
// +kubebuilder:validation:Enum=konnect.konghq.com;aigateway.konghq.com
type AIGatewayRefGroup string

// AIGatewayRefKind is the kind of the AI Gateway (control plane) that an
// AIGatewayRef references.
//
// +kubebuilder:validation:Enum=KonnectAIGateway;OnPremAIGateway
type AIGatewayRefKind string

const (
	// AIGatewayRefGroupKonnect is the API group of the Konnect-hosted
	// KonnectAIGateway.
	AIGatewayRefGroupKonnect AIGatewayRefGroup = "konnect.konghq.com"

	// AIGatewayRefGroupOnPrem is the API group of the on-prem OnPremAIGateway.
	AIGatewayRefGroupOnPrem AIGatewayRefGroup = "aigateway.konghq.com"

	// AIGatewayRefKindKonnect references a KonnectAIGateway.
	AIGatewayRefKindKonnect AIGatewayRefKind = "KonnectAIGateway"

	// AIGatewayRefKindOnPrem references an OnPremAIGateway.
	AIGatewayRefKindOnPrem AIGatewayRefKind = "OnPremAIGateway"
)

// AIGatewayRefType is the type of the reference held by an AIGatewayRef.
//
// +kubebuilder:validation:Enum=namespacedRef
type AIGatewayRefType string

const (
	// AIGatewayRefTypeNamespacedRef references an entity by its namespaced name.
	AIGatewayRefTypeNamespacedRef AIGatewayRefType = "namespacedRef"
)

// AIGatewayRef is the reference to the AI Gateway (control plane) that owns an
// AI Gateway configuration entity.
//
// When Kind is unset it defaults to KonnectAIGateway, and Group resolves to
// the group matching Kind (konnect.konghq.com or aigateway.konghq.com), so
// that existing objects which predate the Group/Kind fields keep referencing
// their KonnectAIGateway.
//
// +kong:channels=kong-operator
// +kubebuilder:validation:XValidation:rule="self.kind == 'OnPremAIGateway' ? (!has(self.group) || self.group == 'aigateway.konghq.com') : (!has(self.group) || self.group == 'konnect.konghq.com')",message="group must be aigateway.konghq.com when kind is OnPremAIGateway, and konnect.konghq.com when kind is KonnectAIGateway"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.kind) || self.kind == oldSelf.kind",message="repointing an entity between KonnectAIGateway and OnPremAIGateway is forbidden"
type AIGatewayRef struct {
	// Type is the type of the reference. Only namespacedRef is supported.
	//
	// Deprecated: kept only for backward compatibility with objects written
	// before the AIGatewayRef type was introduced; it defaults to
	// namespacedRef and will be removed in a future release.
	//
	// +optional
	// +kubebuilder:default=namespacedRef
	Type AIGatewayRefType `json:"type,omitempty"`

	// Group is the API group of the referenced AI Gateway (control plane).
	// When unset, it resolves to the group matching Kind:
	// konnect.konghq.com for KonnectAIGateway, aigateway.konghq.com for
	// OnPremAIGateway.
	//
	// +optional
	Group AIGatewayRefGroup `json:"group,omitempty"`

	// Kind is the kind of the referenced AI Gateway (control plane):
	// KonnectAIGateway (default) or OnPremAIGateway.
	//
	// +optional
	// +kubebuilder:default=KonnectAIGateway
	Kind AIGatewayRefKind `json:"kind,omitempty"`

	// NamespacedRef references the AI Gateway (control plane) by namespaced
	// name. When Namespace is unset, the namespace of the referencing entity
	// is used.
	//
	// +required
	NamespacedRef *commonv1alpha1.NamespacedRef `json:"namespacedRef,omitempty"`
}

// ToObjectRef converts the AIGatewayRef to a commonv1alpha1.ObjectRef with a
// namespacedRef type. The Group/Kind discriminator has no ObjectRef
// representation, so the conversion carries only the namespaced reference.
func (r AIGatewayRef) ToObjectRef() commonv1alpha1.ObjectRef {
	return commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: r.NamespacedRef,
	}
}

// AIGatewayRefFromObjectRef converts a commonv1alpha1.ObjectRef to an
// AIGatewayRef with default (Konnect) Group/Kind. Only namespacedRef-typed
// ObjectRefs carry meaningful data: the Konnect ID reference type has no
// AIGatewayRef representation, and converting one yields an AIGatewayRef with
// a nil NamespacedRef.
func AIGatewayRefFromObjectRef(ref commonv1alpha1.ObjectRef) AIGatewayRef {
	return AIGatewayRef{
		NamespacedRef: ref.NamespacedRef,
	}
}

// EffectiveGroup returns the group of the referenced AI Gateway. When unset,
// it resolves to the group matching Kind: konnect.konghq.com for
// KonnectAIGateway (including objects that predate the Group/Kind fields) and
// aigateway.konghq.com for OnPremAIGateway.
func (r AIGatewayRef) EffectiveGroup() AIGatewayRefGroup {
	if r.Group != "" {
		return r.Group
	}
	if r.EffectiveKind() == AIGatewayRefKindOnPrem {
		return AIGatewayRefGroupOnPrem
	}
	return AIGatewayRefGroupKonnect
}

// EffectiveKind returns the kind of the referenced AI Gateway, applying the
// KonnectAIGateway default for objects that predate the Kind field.
func (r AIGatewayRef) EffectiveKind() AIGatewayRefKind {
	if r.Kind == "" {
		return AIGatewayRefKindKonnect
	}
	return r.Kind
}

// ParentGVK returns the GroupVersionKind of the AI Gateway (control plane)
// this reference points at, with the group/kind defaults applied: group
// konnect.konghq.com for KonnectAIGateway and aigateway.konghq.com for
// OnPremAIGateway.
func (r AIGatewayRef) ParentGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   string(r.EffectiveGroup()),
		Version: aigatewayv1alpha1.GroupVersion.Version,
		Kind:    string(r.EffectiveKind()),
	}
}

// TargetsOnPremAIGateway reports whether the reference resolves to an
// OnPremAIGateway.
func (r AIGatewayRef) TargetsOnPremAIGateway() bool {
	return r.EffectiveKind() == AIGatewayRefKindOnPrem
}

// TargetsKonnectAIGateway reports whether the reference resolves to a
// KonnectAIGateway.
func (r AIGatewayRef) TargetsKonnectAIGateway() bool {
	return r.EffectiveKind() == AIGatewayRefKindKonnect
}
