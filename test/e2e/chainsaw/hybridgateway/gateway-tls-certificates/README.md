# Gateway TLS Certificates Test

This test validates that the hybrid gateway controller correctly processes Gateway TLS listeners and creates the corresponding KongCertificate and KongSNI resources.

## Test Scenario

1. **Prerequisites**: 
   - Creates a Gateway with TLS listener referencing a TLS secret
   - Verifies the secret and namespaces are created

2. **Certificate Resources Verification**:
   - Validates that a KongCertificate is created with:
     - Correct name format: `{gateway-name}-{listener-name}-{secret-namespace}-{secret-name}`
     - Proper labels and annotations for Gateway tracking
     - Owner reference to the Gateway
     - ControlPlaneRef from the Gateway's KonnectExtension
   - Validates that a KongSNI is created with:
     - Correct hostname from the listener
     - Reference to the KongCertificate
     - Proper labels and annotations
     - Owner reference to the Gateway

3. **Update Scenario**:
   - Adds an additional TLS listener to the Gateway
   - Verifies new KongCertificate and KongSNI are created
   - Confirms original resources remain intact

4. **Cleanup**:
   - Removes test resources

## Expected Resources

### KongCertificate
- Name: `kong-foo-https-kong-test-tls-secret`
- Namespace: `kong`
- Spec:
  - `cert_type: secretRef`
  - `secretRef`: Points to the TLS secret
  - `controlPlaneRef`: Points to the Konnect ControlPlane

### KongSNI
- Name: `kong-foo-https-kong-test-tls-secret-example-localdomain-dev`
- Namespace: `kong`
- Spec:
  - `name: example.localdomain.dev` (the hostname)
  - `certificateRef`: Points to the KongCertificate

## Labels and Annotations

All created resources have:
- **Labels**:
  - `gateway-operator.konghq.com/hybrid-gateway/managed-by: Gateway`
  - `gateway-operator.konghq.com/hybrid-gateway/gateways-name: kong-foo`
  - `gateway-operator.konghq.com/hybrid-gateway/gateways-namespace: kong`
- **Annotations**:
  - `gateway-operator.konghq.com/hybrid-gateway/gateways: kong/kong-foo`

## Running the Test

```bash
chainsaw test --test-dir test/e2e/chainsaw/hybridgateway/gateway-tls-certificates
```
