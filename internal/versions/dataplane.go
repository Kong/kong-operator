package versions

import (
	"github.com/kong/semver/v4"
)

// MinimumDataPlaneVersion indicates the bare minimum version of the
// DataPlane component that the operator will support.
var MinimumDataPlaneVersion = semver.MustParse("3.6.0")

// IsDataPlaneImageVersionSupported is a helper intended to validate the
// DataPlane image and indicate if the operator can support it.
//
// Presently only a minimum version is required, there are no limits on the
// maximum. This may change in the future.
//
// The image is expected to follow the format "<image>:<tag>" and only supports
// a provided "<tag>" if it is a semver compatible version.
func IsDataPlaneImageVersionSupported(image string) (bool, error) {
	imageVersion, err := FromImage(image)
	if err != nil {
		return false, err
	}

	return imageVersion.GE(MinimumDataPlaneVersion), nil
}

// MinimumDataPlaneKongConfWatchVersion is the minimum version of the Kong
// Gateway image whose entrypoint reads KONG_CONF_WATCH_PATH and runs the
// in-container kong.conf watcher.
//
// See KAG-9223 and the "Reload Gateway listeners from a watched kong.conf" ERD.
var MinimumDataPlaneKongConfWatchVersion = semver.MustParse("3.17.0")

// IsDataPlaneImageKongConfWatchCapable reports whether the given DataPlane image
// can watch a mounted kong.conf.
//
// A tag the operator cannot parse as a version is reported as capable. Pre-release
// and development images (e.g. "kong/kong-gateway-dev:pr-23439-ubuntu") carry no
// semver tag, and the feature is opt-in: refusing to enable it on an unparseable
// tag would make it impossible to test against exactly the images the Gateway
// team publishes for that purpose. The watcher is a no-op on an image that does
// not implement it, so the cost of being wrong here is that nothing reloads.
func IsDataPlaneImageKongConfWatchCapable(image string) bool {
	imageVersion, err := FromImage(image)
	if err != nil {
		return true
	}

	// Compare without the pre-release/flavour suffix, so that e.g.
	// "3.17.0.0-ubuntu" and "3.17.0-rc.1" both count as 3.17.0.
	return semver.Version{
		Major: imageVersion.Major,
		Minor: imageVersion.Minor,
		Patch: imageVersion.Patch,
	}.GE(MinimumDataPlaneKongConfWatchVersion)
}
