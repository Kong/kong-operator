package versions

import (
	"github.com/kong/semver/v4"
)

// minimumDataPlaneVersion indicates the bare minimum version of the
// DataPlane component that the operator will support.
var minimumDataPlaneVersion = semver.MustParse("3.0.0")

// IsDataPlaneImageVersionSupported is a helper intended to validate the
// DataPlane image and indicate if the operator can support it.
//
// Presently only a minimum version is required, there are no limits on the
// maximum. This may change in the future.
//
// The image is expected to follow the format "<image>:<tag>" and only supports
// a provided "<tag>" if it is a semver compatible version.
func IsDataPlaneImageVersionSupported(image string) (bool, error) {
	imageVersion, err := versionFromImage(image)
	if err != nil {
		return false, err
	}

	return imageVersion.GE(minimumDataPlaneVersion), nil
}
