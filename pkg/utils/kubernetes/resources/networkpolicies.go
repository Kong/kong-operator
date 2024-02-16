package resources

import (
	"reflect"

	v1 "k8s.io/api/networking/v1"
)

// EnsureNetworkPolicyIsUpdated checks if the provided network policy needs an update.
// It comes to a decision by comparing the provided policies' specs.
// It returns a boolean which indicates whether we need to perform an update.
// Note that the provided existing policy is updated in place.
func EnsureNetworkPolicyIsUpdated(
	existing *v1.NetworkPolicy,
	generated *v1.NetworkPolicy,
) bool {
	if reflect.DeepEqual(existing.Spec, generated.Spec) &&
		reflect.DeepEqual(existing.Labels, generated.Labels) &&
		reflect.DeepEqual(existing.Annotations, generated.Annotations) {
		return false
	}

	existing.Spec = generated.Spec
	existing.Labels = generated.Labels
	existing.Annotations = generated.Annotations

	return true
}
