package consts

const (
	// AnnotationSpecHash is the annotation used to store the hash of the spec
	// in the owner object.
	// This is used to detect changes in the spec of the owner object and to prevent
	// unnecessary updates to the child objects when enforce-config is set to false.
	// One exemplar use case for this is AKS where Admission Enforcer mutates
	// ControlPlane's ValidatingWebhookConfiguration.
	AnnotationSpecHash = "gateway-operator.konghq.com/spec-hash"
)
