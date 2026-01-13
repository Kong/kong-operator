# Basic HTTPRoute Test

This test validates the complete lifecycle of an `HTTPRoute` managed by the Hybrid Gateway controller, including creation, update, and traffic routing for multiple endpoints and protocols.

## Test Flow Overview

1. **Create Prerequisite Resources**
    - Deploys two backend applications: `httpbin` and `echo`.
    - Sets up Gateway API resources: `GatewayClass`, `GatewayConfiguration`, and a `Gateway`.
    - Configures Konnect integration using `KonnectAPIAuthConfiguration`.
    - Creates and verifies TLS secrets and certificates for HTTPS traffic.
    - Asserts all deployments and gateway-related resources are ready and programmed.

2. **Create and Verify HTTPRoute**
    - Applies an `HTTPRoute` that routes traffic from the `/echo` path to the `echo` service.
    - Verifies the controller creates the expected Kong resources (`KongRoute`, `KongService`, `KongUpstream`, `KongTarget`) with correct configuration and status.

3. **Traffic Verification (Initial Route)**
    - Sends HTTP and HTTPS requests to `/echo` from both outside (host) and inside (cluster) the cluster.
    - Verifies that both HTTP and HTTPS traffic is correctly routed and receives a `200 OK` response.
    - Checks that HTTPS uses the expected certificate.

4. **Update HTTPRoute and Verify Changes**
    - Updates the `HTTPRoute` to route to the `httpbin` service, adds multiple path and HTTP method matches (`/get`, `/post`, `/put`, `/patch`, `/delete`, `/status`), and disables `strip_path`.
    - Verifies that old Kong resources for the previous route are deleted and new ones are created reflecting the updated rules.

5. **Traffic Verification (Updated Route)**
    - For each endpoint (`/get`, `/post`, `/put`, `/patch`, `/delete`, `/status`), sends requests using all relevant HTTP methods (GET, POST, PUT, PATCH, DELETE) from both host and cluster, over both HTTP and HTTPS.
    - Asserts that all requests receive the expected responses, confirming the data plane is correctly updated and functional for all new routes and methods.

## Notes
- The test is fully automated and self-contained, using a dynamically assigned namespace for isolation.
- All resource creation, assertions, and traffic checks are performed as part of the test scenario.
- Both HTTP and HTTPS traffic are validated for all endpoints and methods, from both inside and outside the cluster.
