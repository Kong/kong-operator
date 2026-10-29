# Upstream mTLS with an existing KongCertificate

For **Konnect-backed Gateway API routes**, select an existing
`configuration.konghq.com/KongCertificate` on the backend Kubernetes Service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: upstream
  namespace: backends
  annotations:
    konghq.com/protocol: https
    konghq.com/client-cert-ref: certificates/upstream-client
spec:
  ports:
    - port: 443
      targetPort: 8443
  selector:
    app: upstream
```

`konghq.com/client-cert-ref: upstream-client` selects a KongCertificate in the
**backend Service namespace**. `namespace/name` selects an explicit namespace.
Empty or malformed references are errors. The generated KongService lives in
the **parent Gateway namespace**, which can differ from both namespaces above.
Its `spec.clientCertificateRef` retains the resolved certificate namespace.

The existing `konghq.com/client-cert` annotation still selects a **TLS Secret**,
not a KongCertificate. Never set both annotations on one Service. When any backend
uses a direct reference, all backends sharing the generated KongService must
select the same KongCertificate: mixing Secret and KongCertificate selection,
different direct references, or a direct reference with an unannotated backend
fails explicitly. Legacy-only configurations retain their first-annotated-backend
selection behavior, including when legacy backends select different Secrets.
Direct selection requires a TLS-capable effective upstream protocol such as
`https`, `grpcs`, or `tls`.

This annotation does not change embedded Kong Ingress Controller behavior or
on-premises Gateway behavior.

## Authorization and validation

For an external certificate in another namespace, create a KongReferenceGrant in
the **certificate namespace**, allowing KongService from the **Gateway namespace**:

```yaml
apiVersion: configuration.konghq.com/v1alpha1
kind: KongReferenceGrant
metadata:
  name: allow-upstream-client
  namespace: certificates
spec:
  from:
    - group: configuration.konghq.com
      kind: KongService
      namespace: gateways
  to:
    - group: configuration.konghq.com
      kind: KongCertificate
      name: upstream-client
```

A grant from `backends` is insufficient when the generated KongService lives in
`gateways`. No grant is required for a same-namespace KongService-to-certificate
reference. Unlike a [Listener certificate reference](gateway-listener-kongcertificate.md),
this selection does **not** require an additional Gateway-to-certificate Gateway
API ReferenceGrant: it is the KongService that references the certificate.

An HTTPRoute referencing a cross-namespace backend Service still needs its usual
Gateway API ReferenceGrant in the **backend namespace**. Cross-namespace
dependencies of the KongCertificate or KongVault separately require their usual
KongReferenceGrants. The Operator does not generate tenant authorization grants
for direct certificate selection.

The certificate must exist, not be deleting, and have current `Programmed=True`
and a Konnect ID. Both its effective ControlPlane and the generated KongService's
ControlPlane must be current and programmed, with the **same remote ControlPlane
ID**. Different Kubernetes ControlPlane mirrors of that remote ControlPlane are
allowed. Selecting a certificate never changes the service's ControlPlane.

Missing grants report `ResolvedRefs=False` with `RefNotPermitted`; other invalid
selections report `InvalidCertificateRef`. The route also reports
`KongConfigurationValid=False` and `KongServiceProgrammed=False`. Invalid rules
are removed, including their previously programmed bindings: revocation does not
silently continue using an old client certificate or fall back to unauthenticated
upstream requests. Revocation cleanup takes precedence over waiting for unrelated
replacement rules. Transient Kubernetes API failures are retried without treating
them as authorization revocation. Watches cover certificate, grant, and backend
Service changes, including restoration after bindings have been removed.

## Vault-backed material and rotation

Use the same material flow as Listener TLS:

1. cert-manager issues a client-auth certificate into a TLS Secret.
2. KonnectConfigStoreSync publishes the pair in `Combined` mode.
3. KongVault exposes the Config Store.
4. An externally managed KongCertificate contains only Vault references.
5. The backend Service selects that KongCertificate.

For example:

```yaml
spec:
  type: inline
  cert: "{vault://upstream-tls/client/certificate}"
  key: "{vault://upstream-tls/client/key}"
  controlPlaneRef:
    type: konnectNamespacedRef
    konnectNamespacedRef:
      name: konnect-control-plane
```

There is no placeholder Secret and no duplicate generated Certificate for direct
selection. The Operator does not adopt, tag, modify, or delete the external
certificate. Certificate rotation propagates through the Config Store and Vault
refresh to new upstream TLS handshakes without replacing the Certificate or
KongService, or restarting DataPlane Pods. Use a Gateway image supporting the
Konnect Vault backend and refresh behavior. Label cert-manager output Secrets with
`konghq.com/secret: "true"` so the default Operator Secret cache sees renewal.
The Combined Config Store entry must fit its 5120-byte limit.

The annotation alone does not hide private keys: use Vault references instead of
inline PEM in the selected KongCertificate. Restrict access to the actual Secret
and Config Store independently. Switching back to `konghq.com/client-cert`
restores the legacy Secret-backed material path, including its non-Vault storage.
Obsolete generated Certificates are removed only after the remote KongService has
switched to its desired certificate binding.

## Verify the upstream server independently

Client certificate selection authenticates **Kong to the upstream**. It does not
configure trust in the upstream server's CA or hostname. Separately configure the
existing server verification settings, for example `konghq.com/tls-verify: "true"`,
the upstream host header/SNI, and the DataPlane's trusted CA bundle. Do not disable
server verification to make client authentication work.
When using the scenario's `konghq.com/host-header` override, its HTTPRoute also
sets `konghq.com/preserve-host: "false"` so the incoming client Host does not
override the upstream's certificate hostname.

The [upstream Chainsaw scenario](../test/e2e/chainsaw/hybridgateway/httproute-kongcertificate)
uses independent client and server CAs and a real TLS upstream requiring a trusted
client certificate. Responses report the verified client's fingerprint, and each
response closes the upstream connection so rotation is tested on new handshakes.
Negative checks cover absent/untrusted clients and wrong server hostname.
It also checks reference-only Konnect Certificate fields, stable entity IDs and
DataPlane Pod identities/restarts, grant revocation/restoration, legacy switching,
and preservation of external certificates.

```shell
make test.e2e.chainsaw \
  CHAINSAW_TEST_DIR=test/e2e/chainsaw/hybridgateway/httproute-kongcertificate
```

The upstream fixture and its positive/negative TLS controls can also be exercised
locally with Python 3 and OpenSSL. This does not replace the live Konnect rotation
test:

```shell
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s test/e2e/chainsaw/common/scripts \
  -p test_verify_upstream_vault_certificate.py -v
```
