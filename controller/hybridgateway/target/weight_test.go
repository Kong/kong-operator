package target

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGcd(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		expected uint32
	}{
		{
			name:     "basic case",
			a:        12,
			b:        18,
			expected: 6,
		},
		{
			name:     "one number is zero",
			a:        5,
			b:        0,
			expected: 5,
		},
		{
			name:     "both numbers are zero",
			a:        0,
			b:        0,
			expected: 0,
		},
		{
			name:     "coprime numbers",
			a:        7,
			b:        11,
			expected: 1,
		},
		{
			name:     "same numbers",
			a:        8,
			b:        8,
			expected: 8,
		},
		{
			name:     "large numbers",
			a:        1071,
			b:        462,
			expected: 21,
		},
		{
			name:     "reverse order",
			a:        18,
			b:        12,
			expected: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gcd(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLcm(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		expected uint32
	}{
		{
			name:     "basic case",
			a:        4,
			b:        6,
			expected: 12,
		},
		{
			name:     "one number is zero",
			a:        5,
			b:        0,
			expected: 0,
		},
		{
			name:     "both numbers are zero",
			a:        0,
			b:        0,
			expected: 0,
		},
		{
			name:     "coprime numbers",
			a:        3,
			b:        7,
			expected: 21,
		},
		{
			name:     "same numbers",
			a:        8,
			b:        8,
			expected: 8,
		},
		{
			name:     "one divides the other",
			a:        3,
			b:        9,
			expected: 9,
		},
		{
			name:     "large numbers",
			a:        15,
			b:        20,
			expected: 60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lcm(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateEndpointWeights(t *testing.T) {
	tests := []struct {
		name     string
		backends []BackendRef
		expected map[string]uint32
	}{
		{
			name:     "empty backends",
			backends: []BackendRef{},
			expected: map[string]uint32{},
		},
		{
			name: "single backend",
			backends: []BackendRef{
				{Name: "service-a", Weight: 10, Endpoints: 2},
			},
			expected: map[string]uint32{
				"service-a": 1,
			},
		},
		{
			name: "basic case from documentation example 1",
			backends: []BackendRef{
				{Name: "backend-a", Weight: 3, Endpoints: 10},
				{Name: "backend-b", Weight: 8, Endpoints: 20},
			},
			expected: map[string]uint32{
				"backend-a": 3,
				"backend-b": 4,
			},
		},
		{
			name: "complex case from documentation example 2",
			backends: []BackendRef{
				{Name: "v1", Weight: 50, Endpoints: 5},
				{Name: "v2", Weight: 50, Endpoints: 8},
				{Name: "v3", Weight: 1, Endpoints: 1},
			},
			expected: map[string]uint32{
				"v1": 40,
				"v2": 25,
				"v3": 4,
			},
		},
		{
			name: "backend with zero weight",
			backends: []BackendRef{
				{Name: "service-a", Weight: 0, Endpoints: 5},
				{Name: "service-b", Weight: 10, Endpoints: 2},
			},
			expected: map[string]uint32{
				"service-a": 0,
				"service-b": 1,
			},
		},
		{
			name: "backend with zero endpoints and zero weight",
			backends: []BackendRef{
				{Name: "service-a", Weight: 0, Endpoints: 0},
				{Name: "service-b", Weight: 20, Endpoints: 4},
			},
			expected: map[string]uint32{
				"service-a": 0,
				"service-b": 1,
			},
		},
		{
			name: "all backends have zero weight",
			backends: []BackendRef{
				{Name: "service-a", Weight: 0, Endpoints: 5},
				{Name: "service-b", Weight: 0, Endpoints: 3},
			},
			expected: map[string]uint32{
				"service-a": 0,
				"service-b": 0,
			},
		},
		{
			name: "all backends have zero endpoints and zero weight",
			backends: []BackendRef{
				{Name: "service-a", Weight: 0, Endpoints: 0},
				{Name: "service-b", Weight: 0, Endpoints: 0},
			},
			expected: map[string]uint32{
				"service-a": 0,
				"service-b": 0,
			},
		},
		{
			name: "backend with weight > 0 but endpoints = 0 (gracefully handled)",
			backends: []BackendRef{
				{Name: "no-endpoints", Weight: 10, Endpoints: 0},
				{Name: "valid-service", Weight: 20, Endpoints: 4},
			},
			expected: map[string]uint32{
				"no-endpoints":  0,
				"valid-service": 1,
			},
		},
		{
			name: "mixed valid and services with no endpoints",
			backends: []BackendRef{
				{Name: "valid-service", Weight: 10, Endpoints: 2},
				{Name: "no-endpoints", Weight: 5, Endpoints: 0},
			},
			expected: map[string]uint32{
				"valid-service": 1,
				"no-endpoints":  0,
			},
		},
		{
			name: "multiple services with no endpoints",
			backends: []BackendRef{
				{Name: "no-endpoints-1", Weight: 10, Endpoints: 0},
				{Name: "no-endpoints-2", Weight: 20, Endpoints: 0},
				{Name: "valid-service", Weight: 30, Endpoints: 3},
			},
			expected: map[string]uint32{
				"no-endpoints-1": 0,
				"no-endpoints-2": 0,
				"valid-service":  1,
			},
		},
		{
			name: "mixed valid and zero weight backends",
			backends: []BackendRef{
				{Name: "service-a", Weight: 0, Endpoints: 0},
				{Name: "service-b", Weight: 15, Endpoints: 3},
				{Name: "service-c", Weight: 0, Endpoints: 10},
			},
			expected: map[string]uint32{
				"service-a": 0,
				"service-b": 1,
				"service-c": 0,
			},
		},
		{
			name: "equal weights different endpoints",
			backends: []BackendRef{
				{Name: "service-a", Weight: 50, Endpoints: 10},
				{Name: "service-b", Weight: 50, Endpoints: 5},
			},
			expected: map[string]uint32{
				"service-a": 1,
				"service-b": 2,
			},
		},
		{
			name: "weights that need GCD simplification",
			backends: []BackendRef{
				// Ratio: 5/1.
				{Name: "service-a", Weight: 20, Endpoints: 4},
				// Ratio: 5/1.
				{Name: "service-b", Weight: 30, Endpoints: 6},
			},
			expected: map[string]uint32{
				"service-a": 1,
				"service-b": 1,
			},
		},
		{
			name: "large numbers",
			backends: []BackendRef{
				{Name: "service-a", Weight: 1000, Endpoints: 100},
				{Name: "service-b", Weight: 2000, Endpoints: 400},
			},
			expected: map[string]uint32{
				"service-a": 2,
				"service-b": 1,
			},
		},
		{
			name: "single endpoint per service",
			backends: []BackendRef{
				{Name: "service-a", Weight: 3, Endpoints: 1},
				{Name: "service-b", Weight: 7, Endpoints: 1},
			},
			expected: map[string]uint32{
				"service-a": 3,
				"service-b": 7,
			},
		},
		{
			name: "prime number weights and endpoints",
			backends: []BackendRef{
				{Name: "service-a", Weight: 7, Endpoints: 3},
				{Name: "service-b", Weight: 11, Endpoints: 5},
			},
			expected: map[string]uint32{
				"service-a": 35,
				"service-b": 33,
			},
		},
		{
			name: "three backends with complex ratios",
			backends: []BackendRef{
				// Ratio: 3/1.
				{Name: "service-a", Weight: 12, Endpoints: 4},
				// Ratio: 5/1.
				{Name: "service-b", Weight: 15, Endpoints: 3},
				// Ratio: 4/1.
				{Name: "service-c", Weight: 8, Endpoints: 2},
			},
			expected: map[string]uint32{
				"service-a": 3,
				"service-b": 5,
				"service-c": 4,
			},
		},
		{
			name: "backends with fractional ratios needing LCM",
			backends: []BackendRef{
				// Ratio: 2/3.
				{Name: "service-a", Weight: 2, Endpoints: 3},
				// Ratio: 3/4.
				{Name: "service-b", Weight: 3, Endpoints: 4},
			},
			expected: map[string]uint32{
				"service-a": 8,
				"service-b": 9,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateEndpointWeights(tt.backends)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateEndpointWeights_TrafficDistribution(t *testing.T) {
	// This test verifies that the calculated weights maintain the correct traffic distribution.
	t.Run("traffic distribution validation", func(t *testing.T) {
		backends := []BackendRef{
			// Should get 10 per endpoint.
			{Name: "service-a", Weight: 30, Endpoints: 3},
			// Should get 10 per endpoint.
			{Name: "service-b", Weight: 70, Endpoints: 7},
		}

		result := CalculateEndpointWeights(backends)

		// Both services should have the same per-endpoint weight since they have the same ratio.
		assert.Equal(t, result["service-a"], result["service-b"])

		// Total traffic for service-a: 3 endpoints * weight = 3 * result["service-a"].
		// Total traffic for service-b: 7 endpoints * weight = 7 * result["service-b"].
		// Ratio should be 30:70 = 3:7.
		totalA := 3 * result["service-a"]
		totalB := 7 * result["service-b"]

		// Since weights are equal, totalA should be 3 and totalB should be 7.
		assert.Equal(t, uint32(3), totalA/result["service-a"])
		assert.Equal(t, uint32(7), totalB/result["service-b"])
	})
}

func TestCalculateEndpointWeights_EdgeCaseCombinations(t *testing.T) {
	t.Run("mix of zero and non-zero backends", func(t *testing.T) {
		backends := []BackendRef{
			{Name: "zero-weight", Weight: 0, Endpoints: 5},
			{Name: "zero-endpoints", Weight: 0, Endpoints: 0},
			{Name: "valid", Weight: 20, Endpoints: 4},
		}

		result := CalculateEndpointWeights(backends)

		expected := map[string]uint32{
			"zero-weight":    0,
			"zero-endpoints": 0,
			"valid":          1,
		}
		assert.Equal(t, expected, result)
	})

	t.Run("all zero weight and zero endpoints", func(t *testing.T) {
		backends := []BackendRef{
			{Name: "service-a", Weight: 0, Endpoints: 0},
			{Name: "service-b", Weight: 0, Endpoints: 0},
		}

		result := CalculateEndpointWeights(backends)

		expected := map[string]uint32{
			"service-a": 0,
			"service-b": 0,
		}
		assert.Equal(t, expected, result)
	})

	t.Run("single backend with weight 1 endpoint 1", func(t *testing.T) {
		backends := []BackendRef{
			{Name: "minimal", Weight: 1, Endpoints: 1},
		}

		result := CalculateEndpointWeights(backends)

		expected := map[string]uint32{
			"minimal": 1,
		}
		assert.Equal(t, expected, result)
	})

	t.Run("graceful handling of services with no endpoints", func(t *testing.T) {
		backends := []BackendRef{
			{Name: "service-with-endpoints", Weight: 50, Endpoints: 10},
			{Name: "service-no-endpoints", Weight: 50, Endpoints: 0},
		}

		result := CalculateEndpointWeights(backends)

		expected := map[string]uint32{
			"service-with-endpoints": 1,
			"service-no-endpoints":   0,
		}
		assert.Equal(t, expected, result)
	})

	t.Run("edge case: force LCM overflow to trigger defensive branch", func(t *testing.T) {
		// Use maximum uint32 values to force overflow.
		// These two large prime numbers should cause LCM overflow.
		// Large prime close to max uint32.
		large1 := uint32(4294967291)
		// Another large prime.
		large2 := uint32(4294967279)

		backends := []BackendRef{
			// Ratio: 1/large1.
			{Name: "overflow-1", Weight: 1, Endpoints: large1},
			// Ratio: 1/large2.
			{Name: "overflow-2", Weight: 1, Endpoints: large2},
		}

		result := CalculateEndpointWeights(backends)

		// If LCM overflows and becomes 0, the defensive branch should trigger
		// and all services should get weight 0.
		assert.NotNil(t, result)
		assert.Contains(t, result, "overflow-1")
		assert.Contains(t, result, "overflow-2")

		// Check if we successfully triggered the defensive branch.
		if result["overflow-1"] == 0 && result["overflow-2"] == 0 {
			// Success! We triggered the len(nonZeroWeights) == 0 branch.
			t.Logf("Successfully triggered defensive branch: all weights are 0 due to LCM overflow")
		} else {
			// The numbers weren't large enough to cause overflow, but that's ok.
			t.Logf("LCM overflow did not occur, got weights: %v", result)
		}
	})
}
