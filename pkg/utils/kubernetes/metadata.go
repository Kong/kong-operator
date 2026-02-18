package kubernetes

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// -----------------------------------------------------------------------------
// Kubernetes Utils - Object Metadata
// -----------------------------------------------------------------------------

// GetAPIVersionForObject provides the string of the full group and version for
// the provided object, e.g. "apps/v1".
func GetAPIVersionForObject(obj client.Object) string {
	if obj.GetObjectKind().GroupVersionKind().Group == "" {
		return obj.GetObjectKind().GroupVersionKind().Version
	}
	return fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Group, obj.GetObjectKind().GroupVersionKind().Version)
}

// EnsureObjectMetaIsUpdated ensures that the existing object metadata has
// all the needed fields set. The source of truth is the second argument of
// the function, a generated object metadata.
func EnsureObjectMetaIsUpdated(
	existingMeta metav1.ObjectMeta,
	generatedMeta metav1.ObjectMeta,
	options ...func(existingMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta),
) (toUpdate bool, updatedMeta metav1.ObjectMeta) {
	var metaToUpdate bool

	// Compare and enforce annotations. Take into account the fact that we don't
	// want to compare all annotations as some might be added by other controllers.
	// We only want to compare the annotations that are added by the operator.
	if generatedMeta.Annotations != nil {
		if existingMeta.Annotations == nil {
			existingMeta.Annotations = make(map[string]string, len(generatedMeta.Annotations))
		}
	}
	for key, value := range generatedMeta.Annotations {
		if existingValue, ok := existingMeta.Annotations[key]; !ok || existingValue != value {
			existingMeta.Annotations[key] = value
			metaToUpdate = true
		}
	}

	// compare and enforce labels
	if !maps.Equal(existingMeta.Labels, generatedMeta.Labels) {
		existingMeta.SetLabels(generatedMeta.GetLabels())
		metaToUpdate = true
	}

	// compare and enforce ownerReferences
	if !slices.EqualFunc(existingMeta.OwnerReferences, generatedMeta.OwnerReferences, func(newObjRef metav1.OwnerReference, genObjRef metav1.OwnerReference) bool {
		sameController := (newObjRef.Controller != nil && genObjRef.Controller != nil && *newObjRef.Controller == *genObjRef.Controller) ||
			(newObjRef.Controller == nil && genObjRef.Controller == nil)
		sameBlockOwnerDeletion := (newObjRef.BlockOwnerDeletion != nil && genObjRef.BlockOwnerDeletion != nil && *newObjRef.BlockOwnerDeletion == *genObjRef.BlockOwnerDeletion) ||
			(newObjRef.BlockOwnerDeletion == nil && genObjRef.BlockOwnerDeletion == nil)
		return newObjRef.APIVersion == genObjRef.APIVersion &&
			newObjRef.Kind == genObjRef.Kind &&
			newObjRef.Name == genObjRef.Name &&
			newObjRef.UID == genObjRef.UID &&
			sameController &&
			sameBlockOwnerDeletion
	}) {
		existingMeta.SetOwnerReferences(generatedMeta.GetOwnerReferences())
		metaToUpdate = true
	}

	// apply all the passed options
	for _, opt := range options {
		var changed bool
		changed, existingMeta = opt(existingMeta, generatedMeta)
		if changed {
			metaToUpdate = true
		}
	}

	return metaToUpdate, existingMeta
}

// TrimGenerateName cut the string to 63 chars, in case it is longer,
// to be compliant with the GenerateName length maximum size of 63 chars.
func TrimGenerateName(name string) string {
	if len(name) > 63 {
		name = name[:62]
	}
	if !strings.HasSuffix(name, "-") {
		name += "-"
	}
	return name
}

// GenerateName generates a name for the object by concatenating the provided base string with a random string of 5 characters.
// It's implementation is based on the Kubernetes GenerateName field ([source]), argument base does not require calling
// TrimGenerateName before, as the function calls it internally.
//
// [source]: https://github.com/kubernetes/kubernetes/blob/d820c046f5c520aab51ec850b263524b1fa5a4e9/staging/src/k8s.io/apiserver/pkg/storage/names/generate.go
func GenerateName(base string) string {
	base = TrimGenerateName(base)

	const (
		maxNameLength          = 63
		randomLength           = 5
		maxGeneratedNameLength = maxNameLength - randomLength
	)
	if len(base) > maxGeneratedNameLength {
		base = base[:maxGeneratedNameLength]
	}
	return fmt.Sprintf("%s%s", base, utilrand.String(randomLength))
}
