# HTTPRoute Path Matching Test

This test validates the HTTPRoute path matching scenarios managed by the Hybrid Gateway controller. It ensures that different path matching types work correctly with the Kong Gateway.

## Test Scenarios

This test covers the following HTTPRoute path matching scenarios:

### Step 1: Prerequisites
- **Goal:** Set up the basic infrastructure.
- Creates the namespace, KonnectAPIAuthConfiguration, GatewayConfiguration, GatewayClass, Gateway, and backend services (echo and httpbin).
- **Verification:** All resources are created and the Gateway is programmed.

### Step 2: PathPrefix Matching
- **Goal:** Test `PathPrefix` path matching type.
- Creates an HTTPRoute with `PathPrefix` matching for `/prefix`.
- **Verification:** 
  - HTTPRoute is Accepted and all conditions are True.
  - Traffic to `/prefix/anything` is routed correctly.
  - Kong resources (KongRoute, KongService, KongUpstream, KongTarget) are created with correct paths.

### Step 3: PathExact Matching
- **Goal:** Test `Exact` path matching type.
- Updates the HTTPRoute to use `Exact` matching for `/exact`.
- **Verification:**
  - HTTPRoute is Accepted and all conditions are True.
  - Traffic to `/exact` returns 200.
  - Traffic to `/exact/subpath` returns 404 (not matched).
  - Kong resources are updated with the new path configuration.

### Step 4: Multiple Path Matches in Single Rule
- **Goal:** Test multiple path matches within a single rule.
- Updates the HTTPRoute to match multiple paths: `/get`, `/post`, `/status`.
- **Verification:**
  - All specified paths route correctly.
  - Kong resources reflect the multiple path configuration.

### Step 5: Path Matching with strip_path Annotation
- **Goal:** Test the `konghq.com/strip-path` annotation.
- Creates an HTTPRoute with `strip-path: "true"` annotation for `/api` prefix.
- **Verification:**
  - Traffic to `/api/get` is routed to `/get` on the backend (path is stripped).
  - Creates another HTTPRoute with `strip-path: "false"` to verify path is preserved.

## Running the Test

```bash
chainsaw test --test-dir test/e2e/chainsaw/hybridgateway/httproute-path-matching
```

## Prerequisites

- Kubernetes cluster with Gateway API CRDs installed
- Kong Gateway Operator installed
- Konnect API token configured (KONNECT_TOKEN environment variable)
