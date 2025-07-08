package ops

import (
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	const (
		// The maximum length of a tag in Konnect.
		maxAllowedTagLength = 128
		// The maximum number of tags that can be attached to a Konnect entity.
		maxAllowedTagsCount = 20
	)

	// Truncate the tags from annotations as we do not validate their length in CEL validations rules.
	var annotationTags []string
	for _, tag := range metadata.ExtractTags(obj) {
		annotationTags = append(annotationTags, truncate(tag, maxAllowedTagLength))
	}

	k8sMetaTags := generateKubernetesMetadataTags(obj)

	// We concatenate the tags in this order to ensure that the k8sMetaTags and additionalTags (from spec) are never
	// truncated below. CEL rules ensure that the total length of k8sMetaTags and additionalTags never exceeds
	// the maximum allowed tags count. That means we will only discard tags from annotations.
	allTags := lo.Uniq(slices.Concat(k8sMetaTags, additionalTags, annotationTags))

	// If the total number of tags exceeds the maximum allowed tags counts, we limit the number of tags to the maximum
	// allowed tags count, discarding the tags from annotations.
	if len(allTags) > maxAllowedTagsCount {
		allTags = allTags[:maxAllowedTagsCount]
	}

	sort.Strings(allTags)
	return allTags
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

	// The maximum length of a tag in Konnect is 128 characters. We truncate them to ensure they are within the limit.
	const maxAllowedValueLength = 128
	tags := make([]string, 0, len(labels))
	for _, label := range labels {
		tags = append(tags, truncate(fmt.Sprintf("%s:%s", label.Key, label.Value), maxAllowedValueLength))
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
	maps.Copy(labels, userSetLabels)

	// The maximum length of a label value in Konnect is 63 characters. We truncate the values to ensure they are
	// within the limit.
	const maxAllowedValueLength = 63
	for k, v := range labels {
		labels[k] = truncate(v, maxAllowedValueLength)
	}

	return labels
}

// UIDLabelForObject returns the Kubernetes UID label and provided object's UID
// separated by a semicolon.
func UIDLabelForObject(obj client.Object) string {
	return fmt.Sprintf("%s:%s", KubernetesUIDLabelKey, obj.GetUID())
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
