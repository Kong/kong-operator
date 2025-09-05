package consts

const (
	// ServiceManagedByLabel indicates that an object's lifecycle is managed
	// by the Service controller.
	ServiceManagedByLabel = "service"

	// HashSpecValueLabel is the label's suffix used to indicate the hash of an object's spec.
	HashSpecValueLabel = "hash-spec"

	// GatewayOperatorHashSpecLabel is the label used to indicate the hash of an object's spec.
	GatewayOperatorHashSpecLabel = OperatorLabelPrefix + HashSpecValueLabel
)
