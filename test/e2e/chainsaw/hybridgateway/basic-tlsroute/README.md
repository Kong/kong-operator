# Basic TLSRoute Test

This test validates the complete lifecycle of a `TLSRoute` managed by the Hybrid Gateway controller, including creation, update, and traffic routing.

## Test Flow Overview

1. **Create Prerequisite Resources**
    - Deploys a backend application: `echo`.
    - Sets up Gateway API resources: `GatewayClass`, `GatewayConfiguration`, and a `Gateway`.
    - Configures Konnect integration using `KonnectAPIAuthConfiguration`.
    - Creates and verifies TLS secrets and certificates for TLS traffic.
    - Asserts all deployments and gateway-related resources are ready and programmed.

2. **Create and Verify TLSRoute**
    - Applies an `TLSRoute` that routes traffic from the `echo.example.com` SNI to the `echo` service.
    - Verifies the controller creates the expected Kong resources (`KongRoute`, `KongService`, `KongUpstream`, `KongTarget`) with correct configuration and status.

3. **Traffic Verification (Initial Route)**
    - Sends TLS traffic with SNI `echo.example.com` on the address that Kong gateway dataplane listens for TLS traffics.
    - Verifies that the TLS request is routed to the `echo` backend.
    - Checks that TLS uses the expected certificate.
    - Sends TLS traffic with another SNI `alter.example.com` on the address and verify that the TLS connection cannot be established.

4. **Update TLSRoute and Verify Changes**
    - Updates the `TLSRoute` to use wildcard SNI `*.example.com` in `spec.hostname`.
    - Verifies that old Kong resources for the previous route are deleted and new ones are created reflecting the updated rules.

5. **Traffic Verification (Updated Route)**
    - Sends TLS traffic with both SNIs `echo.example.com` and `alter.example.com` and verifies that they both works.

## Notes
- The test is fully automated and self-contained, using a dynamically assigned namespace for isolation.
- All resource creation, assertions, and traffic checks are performed as part of the test scenario.
- TLS traffic is validated from both inside and outside the cluster.
