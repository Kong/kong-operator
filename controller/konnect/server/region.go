package server

import (
	"fmt"
	"regexp"
)

// Region represents a Konnect region.
type Region string

func (r Region) String() string {
	return string(r)
}

// These are the well-known Konnect regions. The list is not exhaustive.
const (
	// RegionGlobal represents the global region.
	RegionGlobal Region = "global"
)

var regionRegex = regexp.MustCompile(`^[a-z]{2}$`)

// NewRegion returns a new Region from a string, or an error if the string is not a valid region.
func NewRegion(region string) (Region, error) {
	// A special case for the global region as that's the one that doesn't match the pattern.
	if region == RegionGlobal.String() {
		return RegionGlobal, nil
	}

	// The rest of the regions follow a pattern. We do not validate against specific regions as the list is not exhaustive
	// and may change in the future. This is to avoid having to update the code every time a new region is added.
	// https://github.com/kong/kong-operator/issues/1412 can be considered to make sure that we allow configuring only
	// the regions that are supported by Konnect.
	if !regionRegex.MatchString(region) {
		return "", fmt.Errorf("invalid region %q", region)
	}

	// We can safely convert the string to a Region now.
	return Region(region), nil
}
