package v1alpha1

// GatewayRef is a reference to the gateway (control plane) object that an
// entity belongs to. It identifies the gateway by its API group, kind and name,
// so that the same field can target gateways of different kinds, e.g. a
// Konnect-hosted one or an on-prem one.
//
// The set of group and kind pairs an entity accepts is defined by the entity
// which embeds the reference.
//
// +kong:channels=kong-operator
type GatewayRef struct {
	// Group is the API group of the referred gateway,
	// e.g. konnect.konghq.com.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Group string `json:"group,omitempty"`

	// Kind is the kind of the referred gateway, e.g. KonnectEventGateway.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Kind string `json:"kind,omitempty"`

	// Name is the name of the referred gateway.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// Namespace is the namespace of the referred gateway.
	//
	// If no Namespace is provided then the namespace of the referring object
	// is used.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Namespace *string `json:"namespace,omitempty"`
}

// IsGroupKind reports whether the reference targets the given group and kind.
func (r GatewayRef) IsGroupKind(group, kind string) bool {
	return r.Group == group && r.Kind == kind
}

// ToObjectRef converts the GatewayRef to a namespaced ObjectRef. The group and
// kind have no ObjectRef representation and are dropped.
func (r GatewayRef) ToObjectRef() ObjectRef {
	return ObjectRef{
		Type: ObjectRefTypeNamespacedRef,
		NamespacedRef: &NamespacedRef{
			Name:      r.Name,
			Namespace: r.Namespace,
		},
	}
}
