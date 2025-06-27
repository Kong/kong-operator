package controlplane

import (
	"fmt"
	"testing"

	"github.com/kong/kubernetes-ingress-controller/v3/pkg/telemetry/types"
	"github.com/stretchr/testify/require"
)

func Test_DefaultPayloadCustomizer_WithCustomHostnameRetriever(t *testing.T) {
	hostnameRetriever := func() (string, error) {
		return "custom-host", nil
	}

	customizer, err := defaultPayloadCustomizer(hostnameRetriever)
	require.NoError(t, err)

	payload := types.Payload{"key1": "value1", "v": "1.0"}
	result := customizer(payload)

	require.Equal(t, "value1", result["key1"])
	require.Equal(t, "custom-host", result["hn"])
	_, exists := result["v"]
	require.False(t, exists)
}

func Test_DefaultPayloadCustomizer_WithErrorFromHostnameRetriever(t *testing.T) {
	hostnameRetriever := func() (string, error) {
		return "", fmt.Errorf("hostname error")
	}

	customizer, err := defaultPayloadCustomizer(hostnameRetriever)
	require.Error(t, err)
	require.Nil(t, customizer)
	require.Contains(t, err.Error(), "hostname error")
}

func Test_DefaultPayloadCustomizer_EmptyPayload(t *testing.T) {
	hostnameRetriever := func() (string, error) {
		return "test-host", nil
	}

	customizer, err := defaultPayloadCustomizer(hostnameRetriever)
	require.NoError(t, err)

	result := customizer(types.Payload{})

	require.Len(t, result, 1)
	require.Equal(t, "test-host", result["hn"])
}

func Test_DefaultPayloadCustomizer_PreservesOriginalPayload(t *testing.T) {
	hostnameRetriever := func() (string, error) {
		return "test-host", nil
	}

	customizer, err := defaultPayloadCustomizer(hostnameRetriever)
	require.NoError(t, err)

	original := types.Payload{"v": "1.0", "key1": "value1"}
	originalCopy := types.Payload{"v": "1.0", "key1": "value1"}

	result := customizer(original)

	// Verify original is unchanged
	require.Equal(t, originalCopy, original)
	// Verify result is different
	require.NotEqual(t, original, result)
}

func Test_DefaultPayloadCustomizer_HandlesComplexValues(t *testing.T) {
	hostnameRetriever := func() (string, error) {
		return "test-host", nil
	}

	customizer, err := defaultPayloadCustomizer(hostnameRetriever)
	require.NoError(t, err)

	complexPayload := types.Payload{
		"number":  42,
		"boolean": true,
		"slice":   []string{"a", "b"},
		"map":     map[string]string{"k": "v"},
		"v":       "remove-me",
	}

	complexPayloadCopy := types.Payload{
		"number":  42,
		"boolean": true,
		"slice":   []string{"a", "b"},
		"map":     map[string]string{"k": "v"},
		"v":       "remove-me",
	}
	result := customizer(complexPayload)

	require.Equal(t, 42, result["number"])
	require.Equal(t, true, result["boolean"])
	require.Equal(t, []string{"a", "b"}, result["slice"])
	require.Equal(t, map[string]string{"k": "v"}, result["map"])
	require.Equal(t, "test-host", result["hn"])
	_, hasV := result["v"]
	require.False(t, hasV)

	require.Equal(t, complexPayloadCopy, complexPayload)
}
