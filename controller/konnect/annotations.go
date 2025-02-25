package konnect

const (
	// AnnotationKeyAdoptEntity is the annotation key to adopt an existing Konnect entity.
	AnnotationKeyAdoptEntity = "konnect.konghq.com/adopt"
)

// getAdoptEntityID gets the Konnect ID of the adopted entity in the value of the annotation.
func getAdoptEntityID(annotations map[string]string) string {
	if annotations == nil {
		return ""
	}
	return annotations[AnnotationKeyAdoptEntity]
}
