package consts

const (
	// AnnotationPodTemplateSpecHash is the annotation used to store the hash of the
	// spec of the DataPlane. This is used to detect changes in the spec of the
	// DataPlane and trigger a rolling update.
	AnnotationPodTemplateSpecHash = "gateway-operator.konghq.com/spec-hash"
)
