# HTTPRoute Multiple Backend References Test

This test validates the hybrid gateway controller's handling of `HTTPRoute` with multiple backend references. It ensures proper load balancing and traffic distribution across multiple backends.

The test is divided into the following steps:

**Step 1: Create Prerequisite Resources**

- **Goal:** Set up the initial Kubernetes and Kong environment.
- **Actions:**
    - Creates namespaces for Kong resources and a separate namespace for cross-namespace testing.
    - Deploys multiple backend applications: `echo`, `httpbin`, and `echo-secondary`.
    - Sets up the core Gateway API resources: `GatewayClass`, `GatewayConfiguration`, and a `Gateway`.
    - Configures Konnect integration using `KonnectAPIAuthConfiguration`.
- **Verification:** Asserts that all deployments are ready and all gateway-related resources have a `True` status condition.

**Step 2: Create HTTPRoute with Multiple Backends (Equal Weights)**

- **Goal:** Test routing to multiple backends with equal weight distribution.
- **Actions:**
    - Applies an `HTTPRoute` that routes traffic to two backend services with equal weights.
- **Verification:**
    - Asserts that the `HTTPRoute` status becomes `Accepted` and all `Programmed` conditions are `True`.
    - Verifies that multiple `KongTarget` resources are created, one for each backend.
    - Tests traffic distribution to confirm both backends receive requests.

**Step 3: Update HTTPRoute with Weighted Backend Distribution**

- **Goal:** Test weighted traffic distribution across backends.
- **Actions:**
    - Updates the `HTTPRoute` to use different weights (e.g., 80/20 split).
- **Verification:**
    - Asserts that the Kong resources are updated with correct weights.
    - Verifies traffic is routed successfully.

**Step 4: Test Multiple Backends with Different Ports**

- **Goal:** Test backends using different service ports.
- **Actions:**
    - Updates the `HTTPRoute` to reference backends on different ports.
- **Verification:**
    - Asserts that Kong resources correctly reference the different ports.
    - Verifies traffic is routed successfully.

**Step 5: Test Cross-Namespace Backend Reference with ReferenceGrant**

- **Goal:** Test backend references across namespaces using `ReferenceGrant`.
- **Actions:**
    - Creates a `ReferenceGrant` in the target namespace to allow cross-namespace references.
    - Updates the `HTTPRoute` to reference a backend in a different namespace.
- **Verification:**
    - Asserts that the `HTTPRoute` status shows `ResolvedRefs: True`.
    - Verifies traffic is routed successfully to the cross-namespace backend.
