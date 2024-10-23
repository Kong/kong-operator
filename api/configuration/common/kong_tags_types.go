package common

// Tags is a type for a list of tags applied to a Kong entity.
// +kubebuilder:validation:MaxItems=20
// +kubebuilder:validation:XValidation:message="tags entries must not be longer than 128 characters", rule="self.all(tag, size(tag) >= 1 && size(tag) <= 128)"
type Tags []string
