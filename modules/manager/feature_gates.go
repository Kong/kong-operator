package manager

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// FeatureGate is a named feature that can be enabled via the --feature-gates flag.
type FeatureGate string

const (
	// FeatureGateMCPServer enables the MCPServer controller.
	FeatureGateMCPServer FeatureGate = "mcp-server"

	// FeatureGateDataPlaneKongConfWatch makes the operator render the DataPlane's
	// listen settings into a kong.conf that the Kong Gateway container watches,
	// instead of setting them as KONG_* environment variables.
	//
	// With it enabled, adding, changing or removing a Gateway listener no longer
	// alters the Pod template, so Kong reloads in place instead of the Pods being
	// rolled. It requires a Kong Gateway image that reads KONG_CONF_WATCH_PATH.
	//
	// See KAG-9223.
	FeatureGateDataPlaneKongConfWatch FeatureGate = "dataplane-kong-conf-watch"
)

// allFeatureGates is the exhaustive list of valid feature gates.
var allFeatureGates = []FeatureGate{
	FeatureGateMCPServer,
	FeatureGateDataPlaneKongConfWatch,
}

// FeatureGates holds the set of enabled feature gates parsed from the --feature-gates flag.
type FeatureGates map[FeatureGate]struct{}

// NewFeatureGates parses a comma-separated list of feature gate names.
// An empty string is valid and results in no gates being enabled.
// Returns an error if any name is not a known feature gate.
func NewFeatureGates(s string) (FeatureGates, error) {
	gates := make(FeatureGates)
	if s == "" {
		return gates, nil
	}
	for name := range strings.SplitSeq(s, ",") {
		gate := FeatureGate(strings.TrimSpace(name))
		if !isKnownFeatureGate(gate) {
			return nil, fmt.Errorf("unknown feature gate %q, valid values are: %s",
				gate, validFeatureGateNames())
		}
		gates[gate] = struct{}{}
	}
	return gates, nil
}

// Enabled reports whether the given feature gate is enabled.
func (f FeatureGates) Enabled(gate FeatureGate) bool {
	_, ok := f[gate]
	return ok
}

// String returns the comma-separated, sorted list of enabled feature gates.
func (f FeatureGates) String() string {
	names := make([]string, 0, len(f))
	for g := range f {
		names = append(names, string(g))
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// ValidFeatureGateNames returns the comma-separated list of known feature gate
// names, for use in flag help text.
func ValidFeatureGateNames() string {
	return validFeatureGateNames()
}

func isKnownFeatureGate(gate FeatureGate) bool {
	return slices.Contains(allFeatureGates, gate)
}

func validFeatureGateNames() string {
	names := make([]string, 0, len(allFeatureGates))
	for _, g := range allFeatureGates {
		names = append(names, string(g))
	}
	return strings.Join(names, ", ")
}
