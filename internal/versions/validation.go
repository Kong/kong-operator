package versions

import (
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
)

// ----------------------------------------------------------------------------
// Public Types
// ----------------------------------------------------------------------------

// VersionValidationOption is the function signature to be used
// as option to validate ControlPlane and DataPlane versions
type VersionValidationOption func(version string) (bool, error)

// ----------------------------------------------------------------------------
// Private Helper Functions
// ----------------------------------------------------------------------------

// versionFromImage takes a container image in the format "<image>:<version>"
// and returns a semver instance of the version.
func versionFromImage(image string) (semver.Version, error) {
	splitImage := strings.Split(image, ":")
	if len(splitImage) != 2 {
		return semver.Version{}, fmt.Errorf(`expected "<image>:<tag>" format, got: %s`, image)
	}

	imageVersion, err := semver.Parse(splitImage[1])
	if err != nil {
		return semver.Version{}, fmt.Errorf("could not validate image (%s): %w", image, err)
	}

	return imageVersion, nil
}
