package versions

import (
	"github.com/kong/semver/v4"
)

const (
	// DefaultControlPlaneVersion is the default version of the ControlPlane to use.
	//
	// NOTE: This needs to be a full semver version (i.e. it needs to include
	// the minor and the patch version). The reason for this is that it's used in the
	// tests, e.g. https://github.com/kong/kong-operator/blob/02bd1e11243/test/e2e/environment_test.go#L201-L206
	// and those tests create KIC's URLs for things like roles or CRDs.
	// Since KIC only defines the full tags in its repo (as expected) we cannot use
	// a partial version here, as it would not match KIC's tag.
	DefaultControlPlaneVersion = "3.5.1" // renovate: datasource=docker depName=kong/kubernetes-ingress-controller
)

// minimumControlPlaneVersion indicates the bare minimum version of the
// ControlPlane that can be used by the operator.
var minimumControlPlaneVersion = semver.MustParse("3.3.0")

// ManifestsVersionsForKICVersions is a map that explicitly sets which manifests version to use upon the KIC
// version. It is used in /hack/generators/kic to generate the roles and webhook configuration to be used by KIC.
// This data follows the semver constraint syntax (see https://github.com/Masterminds/semver#basic-comparisons)
// to set the range of KIC versions to be associated with a specific role version.
//
// e.g., for KIC with a version lower than "2.4", but greater or equal to "2.3", version "2.3" of the role is used.
//
// Whenever the KIC manifests are updated and released, that change should be reflected in this map,
// and the generators should be run again to produce the new cluster role and webhook config files.
//
// e.g., when in the future  KIC, and that update will be included in
// the release 5.0, a new entry '">=5.0": "5.0"' should be added to this map, and the previous most
// updated entry should be limited to "<5.0".
var ManifestsVersionsForKICVersions = map[string]semver.Version{
	">=3.4":       semver.MustParse("3.4.2"),
	">=3.3, <3.4": semver.MustParse("3.3.1"),
}

// IsControlPlaneImageVersionSupported is a helper intended to validate the
// ControlPlane image and indicate if the operator can support it.
//
// Presently only a minimum version is required, there are no limits on the
// maximum. This may change in the future.
//
// The image is expected to follow the format "<image>:<tag>" and only supports
// a provided "<tag>" if it is a semver compatible version.
func IsControlPlaneImageVersionSupported(image string) (bool, error) {
	imageVersion, err := FromImage(image)
	if err != nil {
		return false, err
	}

	return imageVersion.GE(minimumControlPlaneVersion), nil
}
