package versions

import (
	"fmt"
	"regexp"
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

var patchVersionNotPresentRE = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// versionFromImage takes a container image in the format "<image>:<version>"
// and returns a semver instance of the version.
func versionFromImage(image string) (semver.Version, error) {
	splitImage := strings.Split(image, ":")
	if len(splitImage) != 2 {
		return semver.Version{}, fmt.Errorf(`expected "<image>:<tag>" format, got: %s`, image)
	}

	rawVersion := strings.TrimPrefix(splitImage[1], "v")
	if patchVersionNotPresentRE.MatchString(rawVersion) {
		rawVersion = fmt.Sprintf("%s.0", rawVersion)
	}

	imageVersion, err := semver.Parse(rawVersion)
	if err != nil {
		return semver.Version{}, fmt.Errorf("could not validate image (%s): %w", image, err)
	}

	return imageVersion, nil
}
