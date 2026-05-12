package v1alpha1

import commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"

// CrossReference describes a configured inter-CR reference on a spec field.
// The Ref field holds the *commonv1alpha1.ObjectRef set by the user; the
// reconciler resolves it to a Konnect ID and stores that in the object's status.
type CrossReference struct {
	// Kind is the Go type name of the referenced CRD, e.g. "EventGatewayBackendCluster".
	Kind string `json:"-"`
	// SpecPath is the dot-separated path from the root of the CR to the field
	// holding the ObjectRef, e.g. "spec.apiSpec.destination".
	SpecPath string `json:"-"`
	// Ref is the ObjectRef set by the user on that field.
	Ref *commonv1alpha1.ObjectRef `json:"-"`
}
