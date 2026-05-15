package konnect

import konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"

// sensitiveDataSecretRefsGetter is implemented by CRD types that have one or
// more sensitive fields backed by Kubernetes Secrets. The reconciler calls
// GetSensitiveDataSecretRefs to enumerate which Secrets (and which keys inside
// them) must be present and valid before the entity can be programmed.
type sensitiveDataSecretRefsGetter interface {
	GetSensitiveDataSecretRefs() []konnectv1alpha1.SensitiveDataSecretRef
}
