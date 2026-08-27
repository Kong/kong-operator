package konnect

import (
	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// sensitiveDataSecretRefs returns the sensitive-data Secret references
// declared on ent's spec (Secrets, and the keys inside them, that must be
// present and valid before the entity can be programmed), and whether ent has
// any at all. Each generated API group package emits its own
// identically-shaped SensitiveDataSecretRef type, so ent's
// GetSensitiveDataSecretRefs method (if any) is detected against every known
// package's copy and converted to the common configurationv1alpha1 shape.
func sensitiveDataSecretRefs(ent any) ([]configurationv1alpha1.SensitiveDataSecretRef, bool) {
	switch r := ent.(type) {
	case interface {
		GetSensitiveDataSecretRefs() []configurationv1alpha1.SensitiveDataSecretRef
	}:
		return r.GetSensitiveDataSecretRefs(), true
	case interface {
		GetSensitiveDataSecretRefs() []aiconfigurationv1alpha1.SensitiveDataSecretRef
	}:
		refs := r.GetSensitiveDataSecretRefs()
		out := make([]configurationv1alpha1.SensitiveDataSecretRef, len(refs))
		for i, x := range refs {
			out[i] = configurationv1alpha1.SensitiveDataSecretRef(x)
		}
		return out, true
	default:
		return nil, false
	}
}
