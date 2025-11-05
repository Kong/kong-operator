package target

// BackendRef holds the information for a single backend service.
// This represents a backendRef in a gateway API route.
type BackendRef struct {
	// A unique identifier for the backend (e.g., k8s Service name).
	Name string
	// The service-level weight assigned in the route.
	Weight uint32
	// The total number of ready endpoints for this service.
	Endpoints uint32
}

// gcd computes the greatest common divisor of two numbers using the Euclidean algorithm.
func gcd(a, b uint32) uint32 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// lcm computes the least common multiple of two numbers with overflow protection.
func lcm(a, b uint32) uint32 {
	if a == 0 || b == 0 {
		return 0
	}
	// To prevent potential overflow, calculate as (a / gcd) * b.
	g := gcd(a, b)
	quotient := a / g

	// Check for overflow before multiplication
	if quotient > 0 && b > (1<<32-1)/quotient {
		// Overflow would occur, return maximum uint32 as a fallback
		// This will be handled gracefully by the caller
		return 1<<32 - 1
	}

	return quotient * b
}

// CalculateEndpointWeights translates a set of service-level weights into the
// smallest possible integer weights for their individual endpoints. This is a crucial
// step for data plane components that need a flat list of weighted
// endpoints to perform traffic splitting.
//
// The core logic ensures that the proportion of traffic sent to each service's
// group of endpoints perfectly matches the original service-level weight distribution.
//
// --- Mathematical Logic ---
//
//  1. Calculate Traffic-per-Endpoint Ratio: For each backend service, the conceptual
//     share of traffic for a single endpoint is proportional to its (ServiceWeight / NumberOfEndpoints).
//     For example, a service with weight 80 and 10 endpoints has a per-endpoint "value" of 8,
//     while a service with weight 100 and 20 endpoints has a per-endpoint "value" of 5.
//
//  2. Convert Ratios to Integers: Since the dataplane requires integer weights, we must
//     convert these fractional ratios into a set of whole numbers that maintain the
//     exact same proportions.
//     - First, each fraction (W/E) is simplified to its lowest terms by dividing
//     the numerator and denominator by their Greatest Common Divisor (GCD).
//     - Then, we find the Least Common Multiple (LCM) of all the denominators from the
//     simplified fractions. This LCM is the smallest number that can be used as a
//     common multiplier to turn all the fractions into integers.
//
//  3. Simplify to Smallest Integers: The resulting integer weights are mathematically
//     correct but might be unnecessarily large (e.g., 300 and 400). To ensure maximum
//     efficiency in the data plane, we simplify them.
//     - We calculate the GCD of this new set of integer weights.
//     - By dividing each weight by this GCD, we arrive at the smallest possible
//     set of integers that preserve the original traffic distribution (e.g., 3 and 4).
//
// --- Edge Cases Handled ---
//
//   - A backend with 0 weight or 0 endpoints is correctly assigned an effective endpoint weight of 0.
//   - This gracefully handles the case where a service has weight > 0 but no ready endpoints.
//   - Overflow Protection: All arithmetic operations are protected against uint32 overflow.
//     If calculations would exceed uint32 limits, values are capped at the maximum, ensuring
//     the algorithm remains stable even with extreme configurations.
//
// --- Example 1: Basic Case ---
//
// - Backend A: {Weight: 3, Endpoints: 10} -> Ratio: 3/10
// - Backend B: {Weight: 8, Endpoints: 20} -> Ratio: 8/20 (simplifies to 2/5)
//
// 1. Fractions are 3/10 and 2/5. Denominators are 10 and 5.
// 2. LCM(10, 5) = 10.
// 3. Multiply each fraction by the LCM:
//   - A: (3/10) * 10 = 3
//   - B: (2/5)  * 10 = 4
//     4. Unsimplified weights are {3, 4}. GCD(3, 4) = 1.
//     5. Final weights are {3, 4}. This means each of A's 10 endpoints gets weight 3,
//     and each of B's 20 endpoints gets weight 4.
//
// --- Example 2: Complex Case ---
//
// - Backend V1: {Weight: 50, Endpoints: 5} -> Ratio: 50/5 (simplifies to 10/1)
// - Backend V2: {Weight: 50, Endpoints: 8} -> Ratio: 50/8 (simplifies to 25/4)
// - Backend V3: {Weight:  1, Endpoints: 1} -> Ratio: 1/1
//
// 1. Fractions are 10/1, 25/4, and 1/1. Denominators are 1, 4, 1.
// 2. LCM(1, 4, 1) = 4.
// 3. Multiply each fraction by the LCM:
//   - V1: (10/1) * 4 = 40
//   - V2: (25/4) * 4 = 25
//   - V3: (1/1)  * 4 = 4
//
// 4. Unsimplified weights are {40, 25, 4}. GCD(40, 25, 4) = 1.
// 5. Final weights are {40, 25, 4}.
func CalculateEndpointWeights(backends []BackendRef) map[string]uint32 {
	if len(backends) == 0 {
		return make(map[string]uint32)
	}

	// This struct holds the simplified fraction: Weight / Endpoints.
	type simplifiedFraction struct {
		id  string
		num uint32
		den uint32
	}

	fractions := make([]simplifiedFraction, 0, len(backends))
	var activeDenominators []uint32

	// Step 1: Validate input and create simplified fractions for each backend.
	for _, be := range backends {
		// Backends with 0 weight or 0 endpoints will get an effective endpoint weight of 0.
		// This handles the case where a service has weight > 0 but no ready endpoints gracefully.
		if be.Weight == 0 || be.Endpoints == 0 {
			fractions = append(fractions, simplifiedFraction{id: be.Name, num: 0, den: 1})
			continue
		}

		// Simplify the fraction W/E by dividing by their GCD.
		commonDivisor := gcd(be.Weight, be.Endpoints)
		num := be.Weight / commonDivisor
		den := be.Endpoints / commonDivisor
		fractions = append(fractions, simplifiedFraction{id: be.Name, num: num, den: den})
		activeDenominators = append(activeDenominators, den)
	}

	if len(activeDenominators) == 0 {
		// This case handles when all backends have 0 weight.
		results := make(map[string]uint32)
		for _, f := range fractions {
			results[f.id] = 0
		}
		return results
	}

	// Step 2: Find the least common multiple (LCM) of all denominators.
	// This gives us a common multiplier to turn all fractions into integers.
	// Note: LCM can grow very large with many different denominators, potentially causing overflow.
	overallLCM := activeDenominators[0]
	for i := 1; i < len(activeDenominators); i++ {
		newLCM := lcm(overallLCM, activeDenominators[i])
		// If LCM calculation hit the overflow cap, we've reached mathematical limits.
		if newLCM == 1<<32-1 {
			// In practice, this should be extremely rare with realistic service configurations.
			overallLCM = newLCM
			break
		}
		overallLCM = newLCM
	}

	// Step 3: Calculate the un-simplified, integer-based weights.
	unsimplifiedWeights := make([]uint32, 0, len(fractions))
	nonZeroWeights := make([]uint32, 0)
	for _, f := range fractions {
		// The weight is the fraction's value multiplied by the LCM.
		// Use uint64 arithmetic to prevent overflow, then check bounds.
		var weight uint32
		if f.num == 0 {
			weight = 0
		} else {
			// Perform calculation in uint64 to detect overflow.
			result := (uint64(f.num) * uint64(overallLCM)) / uint64(f.den)
			if result > uint64(1<<32-1) {
				// Overflow detected, cap at maximum uint32.
				weight = 1<<32 - 1
			} else {
				weight = uint32(result)
			}
		}
		unsimplifiedWeights = append(unsimplifiedWeights, weight)
		if weight > 0 {
			nonZeroWeights = append(nonZeroWeights, weight)
		}
	}

	if len(nonZeroWeights) == 0 {
		// Defensive check: handles edge cases where all calculated weights become 0.
		// This could theoretically occur due to integer overflow in LCM calculations
		// with extremely large denominators, though it should be rare in practice.
		results := make(map[string]uint32)
		for _, f := range fractions {
			results[f.id] = 0
		}
		return results
	}

	// Step 4: Find the GCD of all the new non-zero weights to simplify them.
	overallGCD := nonZeroWeights[0]
	for i := 1; i < len(nonZeroWeights); i++ {
		overallGCD = gcd(overallGCD, nonZeroWeights[i])
	}

	// Step 5: Calculate the final, simplified weights and build the result map.
	results := make(map[string]uint32)
	for i, f := range fractions {
		results[f.id] = unsimplifiedWeights[i] / overallGCD
	}

	// Step 6: Apply Kong weight limit enforcement (max weight = 65535).
	results = enforceKongWeightLimits(results)

	return results
}

// enforceKongWeightLimits applies post-processing scaling to ensure all weights are within Kong's limit of 65535.
// If any weight exceeds this limit, all weights are proportionally scaled down while preserving:
// 1. The relative weight ratios between backends.
// 2. Non-zero participation (weights of 0 stay 0, others become at least 1).
//
// Algorithm:
// 1. Find the maximum weight among all calculated weights.
// 2. If max_weight > 65535, calculate scale factor: 65535 / max_weight.
// 3. Apply scaling: multiply all weights by scale factor and truncate to integer.
// 4. Preserve participation: set any resulting non-zero weight that became 0 to 1.
func enforceKongWeightLimits(weights map[string]uint32) map[string]uint32 {
	const maxKongWeight = 65535

	if len(weights) == 0 {
		return weights
	}

	// Step 1: Find the maximum weight.
	var maxWeight uint32 = 0
	for _, weight := range weights {
		if weight > maxWeight {
			maxWeight = weight
		}
	}

	// Step 2: If all weights are within the limit, no scaling needed.
	if maxWeight <= maxKongWeight {
		return weights
	}

	// Step 3: Calculate scale factor and apply scaling.
	scaleFactor := float64(maxKongWeight) / float64(maxWeight)
	scaledWeights := make(map[string]uint32)

	for name, originalWeight := range weights {
		if originalWeight == 0 {
			// Preserve zero weights.
			scaledWeights[name] = 0
		} else {
			// Scale the weight down.
			scaledWeight := uint32(float64(originalWeight) * scaleFactor)

			// Step 4: Preserve participation - ensure non-zero weights don't become 0.
			if scaledWeight == 0 {
				scaledWeight = 1
			}

			scaledWeights[name] = scaledWeight
		}
	}

	return scaledWeights
}
