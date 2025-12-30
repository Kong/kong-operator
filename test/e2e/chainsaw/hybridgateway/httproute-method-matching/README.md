# HTTPRoute Method Matching Test

This test validates the HTTPRoute method matching scenarios managed by the Hybrid Gateway controller. It ensures that different HTTP method matching works correctly with the Kong Gateway.

## Test Scenarios

This test covers the following HTTPRoute method matching scenarios:

### Step 1: Prerequisites
- **Goal:** Set up the basic infrastructure.
- Creates the namespace, KonnectAPIAuthConfiguration, GatewayConfiguration, GatewayClass, Gateway, and backend service (httpbin).
- **Verification:** All resources are created and the Gateway is programmed.

### Step 2: GET Method Matching
- **Goal:** Test GET HTTP method matching.
- Creates an HTTPRoute with GET method matching for `/get` path.
- **Verification:**
  - HTTPRoute is Accepted and all conditions are True.
  - GET request to `/get` returns 200.
  - POST request to `/get` returns 404 (no matching route).

### Step 3: POST Method Matching
- **Goal:** Test POST HTTP method matching.
- Updates the HTTPRoute to use POST method matching for `/post` path.
- **Verification:**
  - HTTPRoute is Accepted and all conditions are True.
  - POST request to `/post` returns 200.
  - GET request to `/post` returns 404 (no matching route).

### Step 4: PUT Method Matching
- **Goal:** Test PUT HTTP method matching.
- Updates the HTTPRoute to use PUT method matching for `/put` path.
- **Verification:**
  - HTTPRoute is Accepted and all conditions are True.
  - PUT request to `/put` returns 200.
  - GET request to `/put` returns 404 (no matching route).

### Step 5: DELETE Method Matching
- **Goal:** Test DELETE HTTP method matching.
- Updates the HTTPRoute to use DELETE method matching for `/delete` path.
- **Verification:**
  - HTTPRoute is Accepted and all conditions are True.
  - DELETE request to `/delete` returns 200.
  - GET request to `/delete` returns 404 (no matching route).

### Step 6: PATCH Method Matching
- **Goal:** Test PATCH HTTP method matching.
- Updates the HTTPRoute to use PATCH method matching for `/patch` path.
- **Verification:**
  - HTTPRoute is Accepted and all conditions are True.
  - PATCH request to `/patch` returns 200.
  - GET request to `/patch` returns 404 (no matching route).

## Running the Test

```bash
chainsaw test --test-dir test/e2e/chainsaw/hybridgateway/httproute-method-matching
```

## Prerequisites

- Kubernetes cluster with Gateway API CRDs installed
- Kong Gateway Operator installed
- Konnect API token configured (KONNECT_TOKEN environment variable)
