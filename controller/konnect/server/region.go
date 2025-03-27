package server

import "fmt"

// Region represents a Konnect region.
type Region string

func (r Region) String() string {
	return string(r)
}

const (
	// RegionGlobal represents the global region.
	RegionGlobal Region = "global"
	// RegionEU represents the European region.
	RegionEU Region = "eu"
	// RegionUS represents the United States region.
	RegionUS Region = "us"
	// RegionAU represents the Australian region.
	RegionAU Region = "au"
	// RegionME represents the Middle East region.
	RegionME Region = "me"
	// RegionIN represents the Indian region.
	RegionIN Region = "in"
)

// NewRegion returns a new Region from a string, or an error if the string is not a valid region.
func NewRegion(region string) (Region, error) {
	switch region {
	case string(RegionGlobal):
		return RegionGlobal, nil
	case string(RegionEU):
		return RegionEU, nil
	case string(RegionUS):
		return RegionUS, nil
	case string(RegionAU):
		return RegionAU, nil
	case string(RegionME):
		return RegionME, nil
	case string(RegionIN):
		return RegionIN, nil
	default:
		return "", fmt.Errorf("unknown region %q", region)
	}
}
