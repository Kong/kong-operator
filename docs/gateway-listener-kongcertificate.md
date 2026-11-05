# Gateway Listener certificates backed by Kong Vault

For a **Konnect-backed Gateway**, an HTTPS Listener can reference an existing
`configuration.konghq.com/KongCertificate` instead of a Kubernetes Secret:

```yaml
listeners:
  - name: https
    hostname: api.example.com
    port: 443
    protocol: HTTPS
    tls:
      mode: Terminate
      certificateRefs:
        - group: configuration.konghq.com
          kind: KongCertificate
          name: serving
```

Kong Operator creates and owns the Listener's KongSNI, using the Listener hostname.
It does **not** copy, modify, adopt, or delete the referenced KongCertificate.
HTTPRoutes can inherit the Listener hostname; a placeholder TLS Secret and a
manually created KongSNI are not needed.

This is an implementation-specific Gateway API reference type, not a portable
Gateway API core feature. On-premises Gateways still require Secret references.
Existing Secret references retain their behavior: their certificate material is
synchronized to a Konnect Certificate and is not hidden through Vault.

For authentication to TLS backends, see
[upstream mTLS with an existing KongCertificate](gateway-upstream-kongcertificate.md).
Upstream client authentication and Listener server certificates are independent.

## Keep private keys out of Certificate fields

Use the following flow:

1. cert-manager issues and renews a certificate into a TLS Secret.
2. KonnectConfigStoreSync synchronizes the pair to a Config Store in `Combined` mode.
3. A KongVault exposes that Config Store to Kong Gateway.
4. A KongCertificate's `spec.cert` and `spec.key` contain Vault references.
5. The Listener references that KongCertificate.

For example, a sync using `combined.storeKey: serving` and a KongVault with
`prefix: listener-tls` produces these references:

```yaml
spec:
  type: inline
  cert: "{vault://listener-tls/serving/certificate}"
  key: "{vault://listener-tls/serving/key}"
  controlPlaneRef:
    type: konnectNamespacedRef
    konnectNamespacedRef:
      name: konnect-control-plane
```

The real certificate and private key remain in the Kubernetes Secret and Config
Store, not in the Certificate's fields. Restrict access to those storage systems
separately. Direct KongCertificate references alone do not hide private keys: the
referenced Certificate must use Vault references, not inline PEM.

Label the cert-manager output Secret through
`spec.secretTemplate.labels.konghq.com/secret: "true"` so the default Operator
Secret cache can observe issuance and renewal. `Combined` mode validates the pair
before replacing a single Config Store entry; the serialized entry must fit the
Config Store size limit of 5120 bytes. Use a Gateway version supporting the
Konnect Vault backend and its refresh behavior.

## Reference validation and ownership

The certificate and its ControlPlane must have current `Programmed=True`
conditions and Konnect IDs. Its effective remote ControlPlane ID must match the
Gateway's ControlPlane ID. Different Kubernetes ControlPlane objects may mirror
the same remote ControlPlane.

For TLS termination, only one certificate reference per Listener is supported.
TLS Passthrough Listeners ignore `certificateRefs` and do not create or wait for
certificate bindings. For terminating Listeners, `ResolvedRefs=False` reports a
missing, unprogrammed, or wrong-ControlPlane certificate as
`InvalidCertificateRef`, and missing grants as `RefNotPermitted`. Listener
`Programmed=True` additionally requires its generated SNI to be programmed
against the referenced certificate.

When the Gateway and certificate share a namespace, neither grant is required
for the Listener and its generated SNI to reference the certificate.

For a certificate in another namespace, set `certificateRefs[].namespace` and
create **both** grants in the certificate namespace:

- A Gateway API ReferenceGrant allowing `gateway.networking.k8s.io/Gateway` from
  the Gateway namespace to reference the named KongCertificate.
- A KongReferenceGrant allowing `configuration.konghq.com/KongSNI` from the
  Gateway namespace to reference that KongCertificate.

The Operator does not create grants in another tenant's namespace. Any
cross-namespace dependencies of the certificate or Vault also need their usual
KongReferenceGrants.

Certificate and grant changes enqueue affected Gateways. An invalid reference
removes the generated SNI; a transient Kubernetes API error is retried instead of
being treated as permission revocation. Switching the referenced certificate
recreates the SNI because a programmed KongSNI's `certificateRef` is immutable.
Changing the Listener, removing it, or deleting its Gateway does not delete an
externally managed certificate.

## End-to-end coverage

The [Chainsaw scenario](../test/e2e/chainsaw/hybridgateway/gateway-kongcertificate)
uses cert-manager, ConfigStoreSync, a cross-namespace Vault-backed certificate,
and an HTTPRoute without hostnames. It verifies trusted HTTPS with hostname
validation, reference-only Certificate fields in Konnect, certificate rotation
without changing entity IDs or restarting DataPlane Pods, switching between Secret
and KongCertificate references, revocation and restoration of both grants, and
preservation of the external certificate after Gateway deletion.

See the [Chainsaw instructions](../test/e2e/chainsaw/README.md) for environment
setup. Run the scenario against an Operator built with this feature:

```shell
make test.e2e.chainsaw \
  CHAINSAW_TEST_DIR=test/e2e/chainsaw/hybridgateway/gateway-kongcertificate
```
