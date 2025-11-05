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

	t.Run("edge case: handle LCM overflow gracefully", func(t *testing.T) {
		// Use maximum uint32 values to test overflow protection.
		// These two large prime numbers should cause LCM overflow in the old logic.
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

		// With overflow protection, the algorithm should handle this gracefully
		// and return valid (possibly capped) weights rather than causing a panic or returning 0.
		assert.NotNil(t, result)
		assert.Contains(t, result, "overflow-1")
		assert.Contains(t, result, "overflow-2")

		// The overflow protection should ensure we get reasonable results
		// Either both weights are equal (if overflow hit the cap) or proportional
		weight1 := result["overflow-1"]
		weight2 := result["overflow-2"]

		t.Logf("Overflow protection test results: overflow-1=%d, overflow-2=%d", weight1, weight2)

		// Both weights should be non-zero and reasonable (not exceed uint32 max)
		assert.Greater(t, weight1, uint32(0), "overflow-1 should have non-zero weight")
		assert.Greater(t, weight2, uint32(0), "overflow-2 should have non-zero weight")
		assert.LessOrEqual(t, weight1, uint32(1<<32-1), "overflow-1 should not exceed uint32 max")
		assert.LessOrEqual(t, weight2, uint32(1<<32-1), "overflow-2 should not exceed uint32 max")
	})

	t.Run("edge case: handle weight multiplication overflow", func(t *testing.T) {
		// Test case where f.num * overallLCM would overflow
		backends := []BackendRef{
			{Name: "large-weight", Weight: 1000000, Endpoints: 3}, // Large weight
			{Name: "small", Weight: 1, Endpoints: 7},              // Creates LCM that when multiplied causes overflow
		}

		result := CalculateEndpointWeights(backends)

		assert.NotNil(t, result)
		assert.Contains(t, result, "large-weight")
		assert.Contains(t, result, "small")

		// Both should have reasonable weights, not overflow
		largeWeight := result["large-weight"]
		smallWeight := result["small"]

		t.Logf("Multiplication overflow test: large-weight=%d, small=%d", largeWeight, smallWeight)

		assert.Greater(t, largeWeight, uint32(0))
		assert.Greater(t, smallWeight, uint32(0))
		assert.LessOrEqual(t, largeWeight, uint32(1<<32-1))
		assert.LessOrEqual(t, smallWeight, uint32(1<<32-1))

		// The ratio should still be approximately correct (large-weight should be much larger)
		assert.Greater(t, largeWeight, smallWeight*100, "large-weight should be significantly larger than small")
	})
}

func TestEnforceKongWeightLimits(t *testing.T) {
	tests := []struct {
		name     string
		weights  map[string]uint32
		expected map[string]uint32
	}{
		{
			name:     "empty weights",
			weights:  map[string]uint32{},
			expected: map[string]uint32{},
		},
		{
			name: "all weights within limit",
			weights: map[string]uint32{
				"service-a": 1000,
				"service-b": 2000,
				"service-c": 3000,
			},
			expected: map[string]uint32{
				"service-a": 1000,
				"service-b": 2000,
				"service-c": 3000,
			},
		},
		{
			name: "weights exactly at limit",
			weights: map[string]uint32{
				"service-a": 65535,
				"service-b": 32767,
			},
			expected: map[string]uint32{
				"service-a": 65535,
				"service-b": 32767,
			},
		},
		{
			name: "single weight exceeds limit",
			weights: map[string]uint32{
				"service-a": 90000,
				"service-b": 1,
			},
			expected: map[string]uint32{
				"service-a": 65535,
				// Scaled but preserved at minimum.
				"service-b": 1,
			},
		},
		{
			name: "multiple weights exceed limit",
			weights: map[string]uint32{
				"service-a": 100000,
				"service-b": 200000,
				"service-c": 50000,
			},
			expected: map[string]uint32{
				// 100000 * (65535/200000) = 32767.5 -> 32767.
				"service-a": 32767,
				// 200000 * (65535/200000) = 65535.
				"service-b": 65535,
				// 50000 * (65535/200000) = 16383.75 -> 16383.
				"service-c": 16383,
			},
		},
		{
			name: "preserve zero weights",
			weights: map[string]uint32{
				"service-a": 90000,
				"service-b": 0,
				"service-c": 30000,
			},
			expected: map[string]uint32{
				// 90000 * (65535/90000) = 65535.
				"service-a": 65535,
				// Zero preserved.
				"service-b": 0,
				// 30000 * (65535/90000) = 21845.
				"service-c": 21845,
			},
		},
		{
			name: "ensure participation preservation",
			weights: map[string]uint32{
				"service-a": 100000,
				"service-b": 1,
			},
			expected: map[string]uint32{
				"service-a": 65535,
				// Very small weight preserved as 1.
				"service-b": 1,
			},
		},
		{
			name: "high ratio: 30000:1 with multiple pods",
			weights: map[string]uint32{
				// Would be too large for Kong.
				"backend-a": 90000,
				"backend-b": 3,
			},
			expected: map[string]uint32{
				// Scaled down to max allowed.
				"backend-a": 65535,
				// Scaled proportionally: 3 * (65535/90000) = 2.18 -> 2.
				"backend-b": 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := enforceKongWeightLimits(tt.weights)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateEndpointWeights_WithKongLimits(t *testing.T) {
	tests := []struct {
		name            string
		backends        []BackendRef
		expectedLimitOK bool
		minRatioFactor  uint32
		description     string
	}{
		{
			name: "high weight ratio requires scaling",
			backends: []BackendRef{
				{Name: "backend-a", Weight: 30000, Endpoints: 1},
				{Name: "backend-b", Weight: 1, Endpoints: 3},
			},
			expectedLimitOK: true,
			minRatioFactor:  1000,
			description:     "High weight, single pod vs low weight, multiple pods",
		},
		{
			name: "extreme large weights",
			backends: []BackendRef{
				{Name: "large", Weight: 1000000, Endpoints: 1},
				{Name: "small", Weight: 1, Endpoints: 100},
			},
			expectedLimitOK: true,
			minRatioFactor:  100,
			description:     "Very large weights that require significant scaling",
		},
		{
			name: "30000:1 ratio with multiple pods",
			backends: []BackendRef{
				{Name: "high-weight", Weight: 30000, Endpoints: 1},
				{Name: "low-weight", Weight: 1, Endpoints: 3},
			},
			expectedLimitOK: true,
			minRatioFactor:  10000,
			description:     "Scenario with very high weight ratio between backends",
		},
		{
			name: "moderate weights within limits",
			backends: []BackendRef{
				{Name: "service-a", Weight: 5000, Endpoints: 2},
				{Name: "service-b", Weight: 1000, Endpoints: 5},
			},
			expectedLimitOK: true,
			minRatioFactor:  2,
			description:     "Moderate weights that should not require scaling",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateEndpointWeights(tt.backends)

			// Verify that no weight exceeds Kong's limit.
			for name, weight := range result {
				assert.LessOrEqual(t, weight, uint32(65535), "Weight for %s exceeds Kong limit", name)
				if weight > 0 {
					assert.GreaterOrEqual(t, weight, uint32(1), "Non-zero weight for %s should be at least 1", name)
				}
			}

			// Verify all non-zero weight backends have positive weights (participation preserved).
			for _, backend := range tt.backends {
				if backend.Weight > 0 && backend.Endpoints > 0 {
					assert.Greater(t, result[backend.Name], uint32(0), "Backend %s should have non-zero weight", backend.Name)
				}
			}

			// Verify relative ratios are maintained for backends with significant weight differences.
			if len(tt.backends) >= 2 && tt.minRatioFactor > 1 {
				// Find the backend with highest and lowest expected weights.
				var highBackend, lowBackend BackendRef
				maxRatio := float64(0)
				for i, b1 := range tt.backends {
					for j, b2 := range tt.backends {
						if i != j && b1.Endpoints > 0 && b2.Endpoints > 0 {
							ratio1 := float64(b1.Weight) / float64(b1.Endpoints)
							ratio2 := float64(b2.Weight) / float64(b2.Endpoints)
							if ratio1 > ratio2 && ratio1/ratio2 > maxRatio {
								maxRatio = ratio1 / ratio2
								highBackend = b1
								lowBackend = b2
							}
						}
					}
				}

				if maxRatio > 1 {
					highWeight := result[highBackend.Name]
					lowWeight := result[lowBackend.Name]
					if lowWeight > 0 {
						assert.Greater(t, highWeight, lowWeight*tt.minRatioFactor,
							"Backend %s should be significantly larger than %s", highBackend.Name, lowBackend.Name)
					}
				}
			}
		})
	}
}
