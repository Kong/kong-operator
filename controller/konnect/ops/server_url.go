package ops

import "strings"

// ServerURL is a type to represent a server URL.
type ServerURL string

// NewServerURL creates a new ServerURL from a string. It accepts either a full URL or a hostname.
func NewServerURL(u string) ServerURL {
	// If the URL does not have an HTTPs scheme, we prepend it HTTPs.
	// CRD's CEL rules ensure that if the URL has no scheme, it's a valid hostname.
	const defaultScheme = "https://"
	if !strings.HasPrefix(u, defaultScheme) {
		return ServerURL(defaultScheme + u)
	}
	// If the URL is a valid URL, we return it as is.
	// We do not validate it uses HTTPs as it's validated on the CRD level.
	return ServerURL(u)
}

func (s ServerURL) String() string {
	return string(s)
}
