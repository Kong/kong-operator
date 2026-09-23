package gateway

import (
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// maxKonnectLabels is the maximum number of Konnect labels allowed on a
// KonnectGatewayControlPlane or a KonnectExtension's DataPlane, once the
// Gateway and GatewayClass annotation values are merged.
// As mentioned in https://developer.konghq.com/konnect-platform/konnect-labels/,
// A maximum of 5 user-defined labels are allowed on each resource.
// So although we can attach more labels on Konnect gateway control planes, only 5 user-defined labels will be considered.
const maxKonnectLabels = 5

// konnectLabelMaxLen is the maximum length (in bytes) of a Konnect label key
// or value.
const konnectLabelMaxLen = 63

var (
	// dpLabelKeyPattern and dpLabelValuePattern mirror the CEL rules on
	// KonnectExtensionDataPlane.Labels/DataPlaneLabelValue
	// (api/konnect/v1alpha2/konnect_extension_types.go).
	dpLabelKeyPattern   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`)
	dpLabelValuePattern = dpLabelKeyPattern

	// cpLabelKeyPattern mirrors the CEL rule on KonnectGatewayControlPlaneSpec
	// wrapping the vendored CreateControlPlaneRequest.Labels
	// (api/konnect/v1alpha2/konnect_gateway_controlplane_types.go). There is no
	// value-pattern rule for control plane labels.
	cpLabelKeyPattern = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)

	dpReservedKeyPrefixes = []string{"kong", "konnect", "insomnia", "mesh", "kic", "_"}
	cpReservedKeyPrefixes = []string{"k8s", "kong", "konnect", "mesh", "kic", "insomnia", "_"}
)

// parseLabelsAnnotationValue parses a Konnect-labels annotation value of the
// form "key1=value1,key2=value2" into a map. An empty value returns a nil
// map and no error.
func parseLabelsAnnotationValue(value string) (map[string]string, error) {
	if value == "" {
		return nil, nil
	}

	result := make(map[string]string)
	for entry := range strings.SplitSeq(value, ",") {
		parts := strings.Split(entry, "=")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("labels annotation malformed - expected format: key1=value1,key2=value2")
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return result, nil
}

// mergeLabelsWithCap merges override on top of base, with override winning
// on conflicting keys. It returns an error when base alone, override alone,
// or the merged result would exceed maxItems entries.
func mergeLabelsWithCap(base, override map[string]string, maxItems int) (map[string]string, error) {
	if len(override) > maxItems || len(base) > maxItems {
		return nil, fmt.Errorf("too many labels: base has %d, override has %d; maximum is %d", len(base), len(override), maxItems)
	}

	merged := make(map[string]string, len(base)+len(override))
	maps.Copy(merged, base)
	maps.Copy(merged, override)

	if len(merged) <= maxItems {
		return merged, nil
	}

	return nil, fmt.Errorf("too many labels after merging: %d exceeds the maximum of %d", len(merged), maxItems)
}

// resolveKonnectLabels reads annotationKey off both gateway and gatewayClass
// (when non-nil), parses each value, and merges them with gateway taking
// precedence over gatewayClass on conflicting keys (see mergeLabelsWithCap).
func resolveKonnectLabels(gateway *gwtypes.Gateway, gatewayClass *gatewayv1.GatewayClass, annotationKey string) (map[string]string, error) {
	gatewayLabels, err := parseLabelsAnnotationValue(gateway.GetAnnotations()[annotationKey])
	if err != nil {
		return nil, fmt.Errorf("Gateway %s/%s: %w", gateway.Namespace, gateway.Name, err)
	}
	if len(gatewayLabels) > maxKonnectLabels {
		return nil, fmt.Errorf("Gateway %s/%s: too many labels: %d exceeds the maximum of %d", gateway.Namespace, gateway.Name, len(gatewayLabels), maxKonnectLabels)
	}

	var gatewayClassLabels map[string]string
	if gatewayClass != nil {
		gatewayClassLabels, err = parseLabelsAnnotationValue(gatewayClass.GetAnnotations()[annotationKey])
		if err != nil {
			return nil, fmt.Errorf("GatewayClass %s: %w", gatewayClass.Name, err)
		}

		if len(gatewayClassLabels) > maxKonnectLabels {
			return nil, fmt.Errorf("GatewayClass %s: too many labels: %d exceeds the maximum of %d", gatewayClass.Name, len(gatewayClassLabels), maxKonnectLabels)
		}
	}

	return mergeLabelsWithCap(gatewayClassLabels, gatewayLabels, maxKonnectLabels)
}

// validateDPLabels validates labels against the Konnect DataPlane label CEL
// rules (see dpLabelKeyPattern/dpLabelValuePattern above).
func validateDPLabels(labels map[string]string) error {
	return validateKonnectLabels(labels, dpLabelKeyPattern, dpLabelValuePattern, dpReservedKeyPrefixes)
}

// validateCPLabels validates labels against the Konnect control plane label
// CEL rules (see cpLabelKeyPattern above). Unlike DataPlane labels, control
// plane label values have no pattern restriction.
func validateCPLabels(labels map[string]string) error {
	return validateKonnectLabels(labels, cpLabelKeyPattern, nil, cpReservedKeyPrefixes)
}

func validateKonnectLabels(labels map[string]string, keyPattern, valuePattern *regexp.Regexp, reservedPrefixes []string) error {
	if len(labels) > maxKonnectLabels {
		return fmt.Errorf("too many labels: %d exceeds the maximum of %d", len(labels), maxKonnectLabels)
	}

	// Sort for deterministic error messages.
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := validateKonnectLabelKey(key, keyPattern, reservedPrefixes); err != nil {
			return err
		}
		if err := validateKonnectLabelValue(labels[key], valuePattern); err != nil {
			return fmt.Errorf("label %q: %w", key, err)
		}
	}
	return nil
}

func validateKonnectLabelKey(key string, pattern *regexp.Regexp, reservedPrefixes []string) error {
	if len(key) == 0 || len(key) > konnectLabelMaxLen {
		return fmt.Errorf("label key %q must be between 1 and %d characters", key, konnectLabelMaxLen)
	}
	if pattern != nil && !pattern.MatchString(key) {
		return fmt.Errorf("label key %q does not match the required pattern %q", key, pattern.String())
	}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(key, prefix) {
			return fmt.Errorf("label key %q must not start with reserved prefix %q", key, prefix)
		}
	}
	return nil
}

func validateKonnectLabelValue(value string, pattern *regexp.Regexp) error {
	if len(value) == 0 || len(value) > konnectLabelMaxLen {
		return fmt.Errorf("value %q must be between 1 and %d characters", value, konnectLabelMaxLen)
	}
	if pattern != nil && !pattern.MatchString(value) {
		return fmt.Errorf("value %q does not match the required pattern %q", value, pattern.String())
	}
	return nil
}
