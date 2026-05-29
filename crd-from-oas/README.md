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
  configuration.konghq.com/v1alpha1:
    types:
      - path: /v1/event-gateways/{gatewayId}/data-plane-certificates
        name: EventGatewayDataPlaneCertificate
        secretReferences:
          - path: spec.apiSpec.certificate
            type: Secret
            key: tls.crt
      - path: /v1/event-gateways/{gatewayId}/virtual-clusters/{virtualClusterId}/consume-policies
        name: EventGatewayVirtualClusterConsumePolicy
        # Omit fields from generated nested schema types for this API only.
        schemaFieldOmissions:
          EventGatewayModifyHeadersPolicyCreate:
            - parentPolicyID
```

Generated ops infer that a controller-runtime client is needed when
`secretReferences` are configured. For other entities that need to
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

### `ops.getForUID`

Generated `getForUID` helpers are used only as a temporary way to find an
existing Konnect entity after a create conflict. They are expected to go away
once Konnect exposes a proper `managed_by`-style signal (for example via
metadata or tags) that lets us match Kubernetes objects directly.

Prefer the simplest option that the target API supports:

- `ops.useUIDTagFilter: true` when the list endpoint supports filtering by the
  Kubernetes UID tag.
- `ops.getForUID.matchFields` when the entity has a stable identity that can be
  derived from fields on the Kubernetes object and the SDK list response item.
- `ops.skipGetForUID: true` only when the generated matcher cannot express the
  lookup and the entity still needs a hand-written helper.

When `ops.getForUID` is needed, the available knobs are:

- `matchFields`: field-by-field equality checks between the Kubernetes object
  and the SDK response item.
- `listItemsSource: slice`: use this when the SDK list response is a bare slice
  (`resp.<field>`) instead of the usual paginated `resp.<field>.Data` shape.
- `rootUnion`: use this when the match depends on which root-union variant is
  selected in the CRD spec.

Example:

```yaml
types:
  - path: /v1/event-gateways/{gatewayId}/listeners/{eventGatewayListenerId}/policies
    name: EventGatewayListenerPolicy
    ops:
      sdk:
        interface: github.com/Kong/sdk-konnect-go.EventGatewayListenerPoliciesSDK
        fieldName: EventGatewayListenerPolicies
      getForUID:
        listItemsSource: slice
        rootUnion:
          unionField: Spec.APISpec.EventGatewayListenerPolicyConfig
          cases:
            - typeValue: tlsServer
              variantField: EventGatewayTLSListen
              responseTypeValue: tls_server
              matchFields:
                - objectField: Name
                  responseField: GetName()
            - typeValue: forwardToVirtualCluster
              variantField: ForwardToVirtualClust
              responseTypeValue: forward_to_virtual_cluster
              matchFields:
                - objectField: Name
                  responseField: GetName()
      create:
        path: github.com/Kong/sdk-konnect-go/models/operations.CreateEventGatewayListenerPolicyRequest
      update:
        path: github.com/Kong/sdk-konnect-go/models/operations.UpdateEventGatewayListenerPolicyRequest
```

`rootUnion` fields mean:

- `unionField`: the Go field path on the Kubernetes object that contains the
  union wrapper.
- `cases[].typeValue`: the CRD discriminator value stored in that wrapper.
- `cases[].variantField`: the selected variant field inside the wrapper.
- `cases[].responseTypeValue`: the discriminator value returned by the SDK list
  response item.
- `cases[].matchFields`: field comparisons relative to the selected variant
  object and the SDK list response item.

If none of the generated strategies fit, leave `skipGetForUID: true` in the
config and provide a hand-written helper in
`controller/konnect/ops/ops_<entity>_manual.go`.

### TODOs

- Generated conversion functions unit tests have to check the actual conversion logic, not just the presence of the functions and that not error has been returned.

- Research feasibility of generating CRD validation tests from OAS spec. If feasible, implement it. If not, add scaoffolding generation which will require users to fill in the test cases manually.
