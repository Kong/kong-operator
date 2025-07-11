package server

import (
	"fmt"
	"net/url"
	"strings"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

const (
	defaultServerScheme = "https://"
)

// Server is a type to represent a server.
type Server struct {
	hostnameWithoutRegion string
	region                Region
}

// NewServer creates a new Server from a string. It accepts either a full URL or a hostnameWithoutRegion.
func NewServer[T any](serverURL string) (Server, error) {
	// On CEL rules level it's validated a URL is either a valid hostnameWithoutRegion or a URL with https scheme.
	// We can safely assume that if the URL does not have an HTTPs scheme, it's a valid hostnameWithoutRegion.
	hostname := strings.TrimPrefix(serverURL, defaultServerScheme)
	hostnameParts := strings.Split(hostname, ".")
	if len(hostnameParts) == 0 {
		return Server{}, fmt.Errorf("empty server url: %q", serverURL)
	}
	regionPart := hostnameParts[0]

	var region Region

	// NOTE: Code below tries to determine if we need a global API endpoint for the given type.
	// Thus far there's no way to determine that programmatically (easily).
	var t T
	// TODO: add more types here that target the global API endpoint.
	switch any(t).(type) {
	case konnectv1alpha1.KonnectCloudGatewayNetwork:
		region = RegionGlobal
	default:
		var err error
		region, err = NewRegion(regionPart)
		if err != nil {
			return Server{}, fmt.Errorf("failed to parse region from hostname: %w", err)
		}
	}

	s := Server{
		hostnameWithoutRegion: strings.Join(hostnameParts[1:], "."),
		region:                region,
	}

	// Validate the constructed URL.
	_, err := url.Parse(s.URL())
	if err != nil {
		return Server{}, fmt.Errorf("failed to construct valid server URL: %w", err)
	}

	return s, nil
}

// URL returns the full URL representation of the Server.
func (s Server) URL() string {
	const defaultScheme = "https://"
	return fmt.Sprintf("%s%s.%s", defaultScheme, s.region, s.hostnameWithoutRegion)
}

// Region returns the region of the Server.
func (s Server) Region() Region {
	return s.region
}
