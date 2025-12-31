# HTTPRoute Method Matching Test

This test validates the HTTPRoute method matching scenarios managed by the Hybrid Gateway controller. It ensures that different HTTP method matching works correctly with the Kong Gateway.

## Test Scenarios

This test covers the following HTTPRoute method matching scenarios:

### Step 1: Prerequisites
- **Goal:** Set up the basic infrastructure.
- Creates the namespace, KonnectAPIAuthConfiguration, GatewayConfiguration, GatewayClass, Gateway, and backend service (httpbin).
- **Verification:** All resources are created and the Gateway is programmed.

### Step 2-6: Single HTTP Method Matching
Tests each HTTP method individually:

- **Step 2: GET Method Matching**
  - Creates an HTTPRoute with GET method matching for `/get` path.
  - Verifies GET request returns 200, POST request returns 404/405.

- **Step 3: POST Method Matching**
  - Updates HTTPRoute to use POST method matching for `/post` path.
  - Verifies POST request returns 200, GET request returns 404/405.

- **Step 4: PUT Method Matching**
  - Updates HTTPRoute to use PUT method matching for `/put` path.
  - Verifies PUT request returns 200, GET request returns 404/405.

- **Step 5: DELETE Method Matching**
  - Updates HTTPRoute to use DELETE method matching for `/delete` path.
  - Verifies DELETE request returns 200, GET request returns 404/405.

- **Step 6: PATCH Method Matching**
  - Updates HTTPRoute to use PATCH method matching for `/patch` path.
  - Verifies PATCH request returns 200, GET request returns 404/405.

### Step 7: Multiple HTTP Methods in Single Rule
- **Goal:** Test multiple HTTP methods (GET, POST, PUT) in a single rule.
- Creates an HTTPRoute with multiple method matches in one rule on `/anything` path.
- **Verification:**
  - GET, POST, PUT requests to `/anything` return 200.
  - DELETE request to `/anything` returns 404/405 (not in allowed methods).

### Step 8: Method Matching Combined with Path Matching
- **Goal:** Test method matching combined with different path matching across multiple rules.
- Creates an HTTPRoute with multiple rules:
  - Rule 1: GET requests to `/get`
  - Rule 2: POST requests to `/post`
  - Rule 3: Any method to `/anything` (no method restriction)
- **Verification:**
  - GET `/get` returns 200, POST `/get` returns 404/405.
  - POST `/post` returns 200, GET `/post` returns 404/405.
  - GET, POST, DELETE `/anything` all return 200 (no method restriction).

## Running the Test

```bash
chainsaw test --test-dir test/e2e/chainsaw/hybridgateway/httproute-method-matching
```

## Prerequisites

- Kubernetes cluster with Gateway API CRDs installed
- Kong Gateway Operator installed
- Konnect API token configured (KONNECT_TOKEN environment variable)

## Notes

- The `konghq.com/strip-path: "false"` annotation is used on HTTPRoutes to preserve the full path when forwarding to httpbin. Without this, httpbin would receive `/` instead of the matched path.
