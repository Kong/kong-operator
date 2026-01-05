# HTTPRoute Complex Multiple Rules Test

This test validates complex HTTPRoute scenarios that combine multiple matching types in a single HTTPRoute resource, as required by the [HybridGateway project](https://github.com/Kong/kong-operator/issues/2762).

## Test Scenarios

This test covers complex HTTPRoute scenarios combining:

- **Path matching** (Exact and PathPrefix)
- **Header matching** (Exact and RegularExpression)
- **Method matching** (GET, POST, PUT, PATCH, DELETE)
- **Multiple rules** with different matching criteria in a single HTTPRoute

### Step 1: Prerequisites

- **Goal:** Set up the basic infrastructure.
- Creates the namespace, KonnectAPIAuthConfiguration, GatewayConfiguration, GatewayClass, Gateway, and backend service (httpbin).
- **Verification:** All resources are created and the Gateway is programmed.

### Step 2: Create Complex HTTPRoute

Creates a single HTTPRoute with 5 different rules:

1. **Rule 1:** GET `/get` + Header `X-Api-Version: v1`
2. **Rule 2:** POST `/post` (no header requirement)
3. **Rule 3:** Any method on `/api/*` prefix + Header `X-Custom-Header: enabled`
4. **Rule 4:** PUT or PATCH on `/anything/*` prefix
5. **Rule 5:** DELETE `/delete` + Regex Header `X-Delete-Token: ^token-[a-z0-9]+$`

### Step 3: Test Rule 1 - GET with Header

- **Goal:** Test path + method + header combination.
- **Verification:**
  - GET `/get` with `X-Api-Version: v1` returns 200
  - GET `/get` without header returns 404
  - GET `/get` with `X-Api-Version: v2` returns 404
  - POST `/get` with correct header returns 404/405

### Step 4: Test Rule 2 - POST without Header

- **Goal:** Test path + method combination without header requirement.
- **Verification:**
  - POST `/post` returns 200
  - GET `/post` returns 404/405

### Step 5: Test Rule 3 - PathPrefix with Header

- **Goal:** Test PathPrefix + header combination with any method.
- **Verification:**
  - GET `/api/anything` with `X-Custom-Header: enabled` returns 200
  - POST `/api/v1/resource` with `X-Custom-Header: enabled` returns 200
  - GET `/api/anything` without header returns 404
  - GET `/api/test` with wrong header value returns 404

### Step 6: Test Rule 4 - PUT/PATCH with PathPrefix

- **Goal:** Test multiple methods in separate matches with PathPrefix.
- **Verification:**
  - PUT `/anything` returns 200
  - PATCH `/anything` returns 200
  - PUT `/anything/subpath` returns 200
  - GET `/anything` returns 404/405
  - DELETE `/anything` returns 404/405

### Step 7: Test Rule 5 - DELETE with Regex Header

- **Goal:** Test path + method + regex header combination.
- **Verification:**
  - DELETE `/delete` with `X-Delete-Token: token-abc123` returns 200
  - DELETE `/delete` with `X-Delete-Token: token-xyz789` returns 200
  - DELETE `/delete` without header returns 404
  - DELETE `/delete` with `X-Delete-Token: invalid` returns 404
  - DELETE `/delete` with `X-Delete-Token: token-ABC123` returns 404 (uppercase doesn't match regex)
  - GET `/delete` with valid token returns 404/405

## Running the Test

```bash
chainsaw test --test-dir test/e2e/chainsaw/hybridgateway/httproute-complex-multiple-rules
```

## Prerequisites

- Kubernetes cluster with Gateway API CRDs installed
- Kong Gateway Operator installed
- Konnect API token configured (KONNECT_TOKEN environment variable)

## Notes

- The `konghq.com/strip-path: "false"` annotation is used on HTTPRoutes to preserve the full path when forwarding to httpbin.
- This test combines scenarios from:
  - [HTTPRoute Path Matching](../httproute-path-matching/)
  - [HTTPRoute Header Matching](../httproute-header-match/)
  - [HTTPRoute Method Matching](../httproute-method-matching/)
