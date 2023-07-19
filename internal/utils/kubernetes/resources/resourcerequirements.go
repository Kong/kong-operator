package resources

import corev1 "k8s.io/api/core/v1"

// ResourceRequirementsEqual compares two corev1.ResourceRequirements.
// It is needed because sometimes we get objects with '1000m' and sometimes with '1'
// set as values and while those 2 are "different", they are the same in value.
func ResourceRequirementsEqual(a corev1.ResourceRequirements, b corev1.ResourceRequirements) bool {
	if len(a.Claims) != len(b.Claims) {
		return false
	}
	if len(a.Limits) != len(b.Limits) {
		return false
	}
	if len(a.Requests) != len(b.Requests) {
		return false
	}

	return a.Limits.Cpu().Equal(*b.Limits.Cpu()) &&
		a.Limits.Memory().Equal(*b.Limits.Memory()) &&
		a.Requests.Cpu().Equal(*b.Requests.Cpu()) &&
		a.Requests.Memory().Equal(*b.Requests.Memory())
}
