package ops

import (
	"fmt"
	"slices"
	"sort"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

const (
	// KubernetesNamespaceLabelKey is the key for the Kubernetes namespace label.
	KubernetesNamespaceLabelKey = "k8s-namespace"

	// KubernetesNameLabelKey is the key for the Kubernetes name label.
	KubernetesNameLabelKey = "k8s-name"

	// KubernetesUIDLabelKey is the key for the Kubernetes UID label.
	KubernetesUIDLabelKey = "k8s-uid"

	// KubernetesGenerationLabelKey is the key for the Kubernetes generation label.
	KubernetesGenerationLabelKey = "k8s-generation"

	// KubernetesKindLabelKey is the key for the Kubernetes kind label.
	KubernetesKindLabelKey = "k8s-kind"

	// KubernetesGroupLabelKey is the key for the Kubernetes group label.
	KubernetesGroupLabelKey = "k8s-group"

	// KubernetesVersionLabelKey is the key for the Kubernetes version label.
	KubernetesVersionLabelKey = "k8s-version"
)

// ObjectWithMetadata is an interface that accepts an object with Kubernetes metadata and object Kind information.
type ObjectWithMetadata interface {
	metav1.Object
	GetObjectKind() schema.ObjectKind
}

// GenerateTagsForObject generates tags for the given object based on its Kubernetes metadata and annotations.
// An optional set of tags can be passed to be included in the generated tags (e.g. tags from the spec).
// It returns a slice of unique, sorted strings for deterministic output.
func GenerateTagsForObject(obj ObjectWithMetadata, additionalTags ...string) []string {
	var (
		annotationTags = metadata.ExtractTags(obj)
		k8sMetaTags    = generateKubernetesMetadataTags(obj)
		res            = lo.Uniq(slices.Concat(annotationTags, k8sMetaTags, additionalTags))
	)
	sort.Strings(res)
	return res
}

// generateKubernetesMetadataTags generates a list of tags from a Kubernetes object's metadata. The tags are formatted as
// "key:value". These can be attached to a Konnect entity that doesn't support labels, but supports tags (e.g. Route, Service,
// Consumer, etc.).
func generateKubernetesMetadataTags(obj ObjectWithMetadata) []string {
	// Use a list of Entry instead of a builtin map to preserve the order of the labels.
	labels := []lo.Entry[string, string]{
		{Key: KubernetesGenerationLabelKey, Value: fmt.Sprintf("%d", obj.GetGeneration())},
		{Key: KubernetesGroupLabelKey, Value: obj.GetObjectKind().GroupVersionKind().GroupVersion().Group},
		{Key: KubernetesKindLabelKey, Value: obj.GetObjectKind().GroupVersionKind().Kind},
		{Key: KubernetesNameLabelKey, Value: obj.GetName()},
		{Key: KubernetesUIDLabelKey, Value: string(obj.GetUID())},
		{Key: KubernetesVersionLabelKey, Value: obj.GetObjectKind().GroupVersionKind().GroupVersion().Version},
	}
	if k8sNamespace := obj.GetNamespace(); k8sNamespace != "" {
		labels = append(labels, lo.Entry[string, string]{Key: KubernetesNamespaceLabelKey, Value: k8sNamespace})
	}
	tags := make([]string, 0, len(labels))
	for _, label := range labels {
		tags = append(tags, fmt.Sprintf("%s:%s", label.Key, label.Value))
	}
	return tags
}

// WithKubernetesMetadataLabels returns a map of user-provided labels to be assigned to a Konnect entity with the origin
// Kubernetes object's metadata added. These can be assigned to a Konnect entity that supports labels (e.g. ControlPlane).
func WithKubernetesMetadataLabels(obj ObjectWithMetadata, userSetLabels map[string]string) map[string]string {
	labels := map[string]string{
		KubernetesNameLabelKey:       obj.GetName(),
		KubernetesUIDLabelKey:        string(obj.GetUID()),
		KubernetesGenerationLabelKey: fmt.Sprintf("%d", obj.GetGeneration()),
		KubernetesKindLabelKey:       obj.GetObjectKind().GroupVersionKind().Kind,
		KubernetesGroupLabelKey:      obj.GetObjectKind().GroupVersionKind().GroupVersion().Group,
		KubernetesVersionLabelKey:    obj.GetObjectKind().GroupVersionKind().GroupVersion().Version,
	}
	if k8sNamespace := obj.GetNamespace(); k8sNamespace != "" {
		labels[KubernetesNamespaceLabelKey] = k8sNamespace
	}
	for k, v := range userSetLabels {
		labels[k] = v
	}
	return labels
}
