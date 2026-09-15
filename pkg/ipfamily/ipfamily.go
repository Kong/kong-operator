package ipfamily

import "fmt"

// IPFamily identifies which IP address families a DataPlane's Kong listens
// should bind to.
type IPFamily string

const (
	// Auto means the IP family is not explicitly configured and should be
	// detected from the cluster (see Detect). This is the default.
	Auto IPFamily = "auto"
	// IPv4 means only the IPv4 wildcard address should be used.
	IPv4 IPFamily = "ipv4"
	// IPv6 means only the IPv6 wildcard address should be used.
	IPv6 IPFamily = "ipv6"
	// Dual means both the IPv4 and IPv6 wildcard addresses should be used.
	Dual IPFamily = "dual"
)

// String returns the string representation of the IPFamily.
func (f IPFamily) String() string {
	return string(f)
}

// New creates a new IPFamily from a string, validating it against the known
// values.
func New(value string) (IPFamily, error) {
	switch f := IPFamily(value); f {
	case Auto, IPv4, IPv6, Dual:
		return f, nil
	default:
		return "", fmt.Errorf("invalid IP family: %s", value)
	}
}
