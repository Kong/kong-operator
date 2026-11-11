package platform

import "fmt"

// Platform identifies the k8s platform flavor the operator runs on. It
// decides whether admission assigns the container user and group (OpenShift
// SecurityContextConstraints) or the operator must pin them.
type Platform string

const (
	// Auto means the platform is not explicitly configured and should be
	// detected by probing the apiserver. This is the default.
	Auto Platform = "auto"
	// Openshift is an OpenShift cluster: admission assigns the container user
	// and group through SecurityContextConstraints.
	Openshift Platform = "openshift"
	// Kubernetes is a cluster without platform-assigned users: vanilla
	// Kubernetes and distributions that do not serve the OpenShift APIs.
	Kubernetes Platform = "kubernetes"
)

// String returns the string representation of the Platform.
func (p Platform) String() string {
	return string(p)
}

// New creates a new Platform from a string, validating it against the known
// values.
func New(value string) (Platform, error) {
	switch p := Platform(value); p {
	case Auto, Openshift, Kubernetes:
		return p, nil
	default:
		return "", fmt.Errorf("invalid platform: %s", value)
	}
}
