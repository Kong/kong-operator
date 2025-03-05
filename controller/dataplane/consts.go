package dataplane

const (
	// AnnotationDataPlaneSpecHash is the annotation used to store the hash of the
	// spec of the DataPlane. This is used to detect changes in the spec of the
	// DataPlane and trigger a rolling update.
	AnnotationDataPlaneSpecHash = "gateway-operator.konghq.com/spec-hash"
)
