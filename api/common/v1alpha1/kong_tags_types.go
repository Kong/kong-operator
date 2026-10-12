package v1alpha1

// Tags is a type for a list of tags applied to a Kong entity.
// Tags starting with the prefixes reserved for the Kubernetes metadata of the source object
// (k8s-group:, k8s-version:, k8s-kind:, k8s-namespace:, k8s-name:, k8s-uid:, k8s-generation:, managed-by:)
// are not allowed as the operator sets them on the Konnect entities.
// +kubebuilder:validation:MaxItems=20
// +kubebuilder:validation:XValidation:message="tags entries must not be longer than 128 characters", rule="self.all(tag, size(tag) >= 1 && size(tag) <= 128)"
// +kubebuilder:validation:XValidation:message="tags entries must not start with reserved prefixes: k8s-group:, k8s-version:, k8s-kind:, k8s-namespace:, k8s-name:, k8s-uid:, k8s-generation:, managed-by:", rule="self.all(tag, !tag.startsWith('k8s-group:') && !tag.startsWith('k8s-version:') && !tag.startsWith('k8s-kind:') && !tag.startsWith('k8s-namespace:') && !tag.startsWith('k8s-name:') && !tag.startsWith('k8s-uid:') && !tag.startsWith('k8s-generation:') && !tag.startsWith('managed-by:'))"
type Tags []string
