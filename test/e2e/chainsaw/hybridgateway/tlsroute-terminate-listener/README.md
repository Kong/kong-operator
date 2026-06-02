# TLSRoute with `Terminate` Listener Test

This test validates that the Hybrid Gateway controller correctly processes a
`TLSRoute` attached to a Gateway listener using `protocol: TLS` with
`tls.mode: Terminate`. In this mode the gateway terminates the inbound TLS
connection using a certificate from the referenced TLS secret and forwards
plain TCP traffic to the backend echo service.

## Test Flow Overview

1. **Create Prerequisite Resources**
    - Creates the TLS secret used by the listener for terminating TLS.
    - Sets up Konnect integration: auth secret and `KonnectAPIAuthConfiguration`.
    - Sets up Gateway API resources: `GatewayConfiguration`, `GatewayClass`,
      and a `Gateway` with a single listener `protocol: TLS`,
      `tls.mode: Terminate` on port 8899.
    - Asserts the Gateway is `Accepted` / `Programmed`, and that the
      `KonnectGatewayControlPlane`, `KongCertificate`, and `KongSNI` resources
      are created and reconciled.

2. **Deploy Backend and TLSRoute**
    - Deploys a plain-TCP `echo` backend (no TLS termination at the pod,
      since the gateway terminates TLS upstream).
    - Applies a `TLSRoute` selecting the gateway listener via `parentRefs`
      and routing SNI `echo.<ns>.example.com` to the echo service on
      its plain TCP port (1025).
    - Verifies the controller creates the expected Kong resources
      (`KongRoute`, `KongService`, `KongUpstream`, `KongTarget`) with
      correct status.

3. **Traffic Verification**
    - From the host, performs an `openssl s_client` TLS handshake to the
      gateway proxy IP on port 8899 with SNI `echo.<ns>.example.com`.
    - Asserts the echo backend's welcome message is received, confirming
      that the gateway terminated TLS and forwarded traffic to the pod.

## Notes
- The scenario differs from `basic-tlsroute`, which uses `tls.mode:
  Passthrough` and a TLS-terminating backend on port 1030. Here the gateway
  itself terminates TLS, so the backend uses the plain TCP port 1025.
- All resource creation, assertions, and traffic checks run inside a
  dynamically assigned namespace for isolation.
