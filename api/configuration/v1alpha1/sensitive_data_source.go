package v1alpha1

import commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"

// SensitiveDataSecretRef identifies a specific key inside a Kubernetes Secret
// that holds a sensitive value for a CRD field.
type SensitiveDataSecretRef struct {
	// Ref is the namespaced reference to the Secret.
	// +optional
	Ref commonv1alpha1.NamespacedRef `json:"ref,omitzero"`
	// Key is the data key within the Secret.
	// +optional
	Key string `json:"key,omitzero"`
}
