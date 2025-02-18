package ops

import (
	"strings"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// ServerURL is a type to represent a server URL.
type ServerURL string

// NewServerURL creates a new ServerURL from a string. It accepts either a full URL or a hostname.
func NewServerURL[T any](u string) ServerURL {
	// NOTE: Code below tries to determine if we need a global API endpoint for the given type.
	// Thus far there's no way to determine that programmatically (easily).
	var t T
	// TODO: add more types here that target the global API endpoint.
	switch any(t).(type) {
	case konnectv1alpha1.KonnectCloudGatewayNetwork:
		u = replaceFirstSegmentToGlobal(u)
	default:
	}

	// If the URL does not have an HTTPs scheme, we prepend it.
	// CRD's CEL rules ensure that if the URL has no scheme, it's a valid hostname.
	const defaultScheme = "https://"
	if !strings.HasPrefix(u, defaultScheme) {
		return ServerURL(defaultScheme + u)
	}
	// If the URL is a valid URL, we return it as is.
	// We do not validate it uses HTTPs as it's validated on the CRD level.
	return ServerURL(u)
}

// String returns the string representation of the ServerURL.
func (s ServerURL) String() string {
	return string(s)
}

// replaceFirstSegmentToGlobal replaces the first segment of the hostname to "global".
// For example:
// - "us.api.konghq.com" -> "global.api.konghq.com"
// - "eu.api.konghq.com" -> "global.api.konghq.com"
func replaceFirstSegmentToGlobal(u string) string {
	parts := strings.Split(u, ".")
	if len(parts) == 0 {
		return u
	}
	parts[0] = "global"
	return strings.Join(parts, ".")
}
