## crd-from-oas

This is a tool to generate Kubernetes CRD definitions and conversion functions
from OpenAPI Specification (OAS) files.
It is designed to help create CRDs that are consistent with API specifications,
reducing the manual effort required to maintain CRD definitions and ensuring that
they stay up-to-date with the API changes.

### Exemplar config

```yaml
apiGroupVersions:
  konnect.konghq.com/v1alpha1:
    commonTypes:
      objectRef:
        import:
          path: github.com/kong/kong-operator/v2/api/common/v1alpha1
          alias: commonv1alpha1
    types:
      - path: /v1/event-gateways
        name: KonnectEventGateway
      - path: /v1/event-gateways/{gatewayId}/data-plane-certificates
        name: KonnectEventDataPlaneCertificate
        optionalSecretReference: true
      - path: /v3/portals
        cel:
          name:
            _validations:
              - "+kubebuilder:validation:XValidation:rule=\"self == oldSelf\",message=\"name is immutable\""
        ops:
          create:
            path: github.com/Kong/sdk-konnect-go/models/components.CreatePortal
          update:
            path: github.com/Kong/sdk-konnect-go/models/components.UpdatePortal
      - path: /v3/portals/{portalId}/teams
```

Generated ops infer that a controller-runtime client is needed when
`optionalSecretReference: true` is enabled. For other entities that need to
read cluster state while building SDK requests, set `ops.requireClient: true`
under the type configuration.

For reconciler-enabled child entities, `crd-from-oas` also generates shared
`<ParentAccessor>Ref*` condition constants in the target API package. The
prefix follows the immediate parent accessor alias derived from the path (for
example `EventGatewayRef*` for `/v1/event-gateways/{gatewayId}/...` children).

The prefix derivation prefers the accessor alias over the raw OAS entity name
(e.g. accessor `EventGateway` is chosen over entity `Gateway`), but keeps the
entity name when it already embeds the accessor as a prefix or suffix
(e.g. entity `EventGatewayListener` with accessor `Listener` stays
`EventGatewayListener`). If a future entity name combines an unrelated prefix
with an accessor that should drive the condition (e.g. entity `KongPortal`
with accessor `Portal`), the heuristic will pick the entity name and emit
`KongPortalRef*` rather than `PortalRef*`. When that conflicts with an
existing convention, prefer to align the OAS accessor alias instead of layering
overrides here.

### TODOs

- Generated conversion functions unit tests have to check the actual conversion logic, not just the presence of the functions and that not error has been returned.

- Research feasibility of generating CRD validation tests from OAS spec. If feasible, implement it. If not, add scaoffolding generation which will require users to fill in the test cases manually.
