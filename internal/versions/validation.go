package versions

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kong/semver/v4"

	kgoerrors "github.com/kong/gateway-operator/internal/errors"
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

var (
	semverPatchVersionNotPresentRE = regexp.MustCompile(`^([0-9]+\.[0-9]+)(-.+?)`)
	semverKongEnterpriseRE         = regexp.MustCompile(`^([0-9]+\.[0-9]+)(\.[0-9]+)?(\.[0-9]+)?(-.+)?$`)
)

// versionFromImage takes a container image in the format "<image>:<version>"
// and returns a semver instance of the version.
// It supports semver with the extension of enterprise segment, being an additional
// forth segment on top the standard 3 segment supported by semver.
// This also supports flavour suffixes which can be supplied after "-" character.
func versionFromImage(image string) (semver.Version, error) {
	splitImage := strings.Split(image, ":")
	if len(splitImage) != 2 {
		return semver.Version{}, fmt.Errorf(`expected "<image>:<tag>" format, got: %s`, image)
	}

	rawVersion := strings.TrimPrefix(splitImage[1], "v")

	// If we matched a semver without patch version with suffix e.g. 3.3-ubuntu
	// then append ".0" before the flavour suffix for successful semver parsing.
	// Hence 3.3-ubuntu becomes 3.3.0-ubuntu.
	res := semverPatchVersionNotPresentRE.FindStringSubmatch(rawVersion)
	switch len(res) {
	case 0:
		break
	case 1:
		rawVersion = fmt.Sprintf("%s.0", rawVersion)
	default:
		rawVersion = fmt.Sprintf("%s.0%s", res[1], res[2])
	}

	res = semverKongEnterpriseRE.FindStringSubmatch(rawVersion)
	switch len(res) {
	case 0, 1:
		return semver.Version{}, fmt.Errorf("%w (image %s)", kgoerrors.ErrInvalidSemverVersion, image)
	default:
		str := res[1]
		// Add patch if specified, otherwise "0"
		if res[2] != "" {
			str += res[2]
		} else {
			str += ".0"
		}
		// Add revision if specified
		if res[3] != "" {
			str += res[3]
		}

		imageVersion, err := semver.Parse(str)
		if err != nil {
			return semver.Version{}, fmt.Errorf("%w (image %s): %w", kgoerrors.ErrInvalidSemverVersion, image, err)
		}
		return imageVersion, nil
	}
}
