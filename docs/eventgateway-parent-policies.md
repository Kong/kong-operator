# Event Gateway parent policies

`encryptFields` produce policies and `decryptFields` consume policies operate
on records parsed by a `schemaValidation` policy. Set
`spec.apiSpec.parentPolicyRef.name` to the Kubernetes name of that parent policy.
The operator resolves the reference to Konnect's `parent_policy_id`.

The parent must have `apiSpec.type: schemaValidation`, be the same kind as the
child (produce or consume), and reference the same virtual cluster in the same
namespace. `parentPolicyRef` uses a namespaced reference with `name`, optional
`kind` (defaulting to the child's kind), and optional `namespace` (defaulting to
the child's namespace). Cross-namespace parent references are rejected during
reference resolution (`KonnectReferencesResolved=False`).

| Child policy type | `parentPolicyRef` |
|-------------------|-------------------|
| Produce `encryptFields` | Required |
| Consume `decryptFields` | Required |
| Produce or consume `modifyHeaders` | Optional |
| Consume `skipRecord` | Optional |
| Other policy types | Not supported |

The reference belongs directly under `apiSpec`, not inside the policy variant.
For example, given an `EventGatewayVirtualCluster` named `vc` and a Konnect
static key named `encryption-key` in that gateway:

```yaml
apiVersion: configuration.konghq.com/v1alpha1
kind: EventGatewayVirtualClusterProducePolicy
metadata:
  name: parse-produce
  namespace: default
spec:
  eventGatewayVirtualClusterRef:
    type: namespacedRef
    namespacedRef:
      name: vc
  apiSpec:
    type: schemaValidation
    schemaValidation:
      config:
        type: json
        json:
          valueValidationAction: reject
---
apiVersion: configuration.konghq.com/v1alpha1
kind: EventGatewayVirtualClusterProducePolicy
metadata:
  name: encrypt-secret
  namespace: default
spec:
  eventGatewayVirtualClusterRef:
    type: namespacedRef
    namespacedRef:
      name: vc
  apiSpec:
    type: encryptFields
    parentPolicyRef:
      name: parse-produce
    encryptFields:
      config:
        failureMode: reject
        encryptFields:
          - paths:
              type: array
              array:
                - match: secret
            encryptionKey:
              type: static
              static:
                key:
                  name: encryption-key
---
apiVersion: configuration.konghq.com/v1alpha1
kind: EventGatewayVirtualClusterConsumePolicy
metadata:
  name: parse-consume
  namespace: default
spec:
  eventGatewayVirtualClusterRef:
    type: namespacedRef
    namespacedRef:
      name: vc
  apiSpec:
    type: schemaValidation
    schemaValidation:
      config:
        type: json
        json:
          valueValidationAction: skip
---
apiVersion: configuration.konghq.com/v1alpha1
kind: EventGatewayVirtualClusterConsumePolicy
metadata:
  name: decrypt-secret
  namespace: default
spec:
  eventGatewayVirtualClusterRef:
    type: namespacedRef
    namespacedRef:
      name: vc
  apiSpec:
    type: decryptFields
    parentPolicyRef:
      name: parse-consume
    decryptFields:
      config:
        failureMode: error
        decryptFields:
          paths:
            type: array
            array:
              - match: secret
        keySources:
          - type: static
            static: {}
```

## Creation, updates, and deletion

Parents and children may be applied together. A child reports
`KonnectReferencesResolved=False` until its parent exists in Konnect. Changes
to the parent trigger reconciliation of referencing children.

New `encryptFields` and `decryptFields` policies must have a parent reference.
Existing parent-less policies from an older operator version can still be
updated or deleted after an upgrade, so their finalizers can be removed.
They remain unprogrammable until a parent reference is added. Once repaired,
the required reference cannot be removed, even before programming.

Once a child has a Konnect ID (`status.id`), its `parentPolicyRef` cannot be
added, changed, or removed: Konnect ignores changes to `parent_policy_id`.
Delete and recreate the child to use a different parent.

Delete children before their schema-validation parent. Konnect would
cascade-delete child policies, so the operator retains the parent's finalizer and reports
`Programmed=False` with reason `DeletionBlocked` while Kubernetes child policy
objects reference it. This includes unprogrammed and terminating children.
Deleting the last child triggers reconciliation and unblocks parent deletion.
An invalid reference to a policy that is not a schema-validation policy in
Konnect does not block deletion, since that policy cannot have remote children.

## Cluster policy-chain API

`EventGatewayVirtualClusterPolicyChain` declares the execution order of all
cluster-level policies in one `EventGatewayVirtualCluster`. A chain corresponds
to an execution scope, not a policy subtype: the same chain can include ACL
and request-validation policies.
The resource defines singleton configuration on its parent, not an independent
Konnect entity. `spec.eventGatewayVirtualClusterRef` must use `namespacedRef` and
cannot change, even before programming. To manage another virtual cluster,
create a new chain CR.

`spec`, `spec.apiSpec`, and `spec.apiSpec.policies` must be supplied explicitly.
Each list entry references an `EventGatewayVirtualClusterPolicy` CR by `name`,
with optional `kind` and `namespace`. An omitted namespace defaults to the chain's
namespace; explicitly specifying the same namespace is allowed. Cross-namespace
references are not supported, and every policy must reference the same virtual
cluster as the chain. List order, not resource names or YAML document order,
determines execution order. See the
[complete example](../config/samples/konnect_eventgateway_virtualcluster_policy_chain.yaml).

The API models complete-set reordering: reference every existing cluster-level
policy exactly once. An explicit `policies: []` is meaningful only when there
are no policies; it is not a request to clear an existing chain. Omitted and
null lists are invalid. Newly created policies must be added to the declaration,
and deleted policies must be removed.

Backend validation against `eu.api.konghq.tech` on 2026-10-22 confirmed that
complete permutations are accepted; partial lists, duplicates, unknown IDs, and
empty lists for nonempty chains are rejected without changing the remote chain.
Missing and null policy lists were also rejected. Duplicate and unknown IDs were
rejected even when the submitted list had the correct number of entries.
New policies were appended to the existing order, and deleted policies were
removed without reordering the remaining policies. This is evidence from the
tested development backend, not a guarantee for every production version.
The SDK v0.73.1 OAS does not declare a policy-count maximum; successful reordering
of 16 distinct policies does not establish an upper limit.

The shared status structure is retained for compatibility with the operator's
Konnect interfaces, but `status.id` must remain unset: there is no independent
chain ID. Conditions, organization/server information, and parent IDs describe
the observed state instead.

### Konnect reconciliation

Each reference must identify a distinct, programmed cluster-level policy in
the same namespace and virtual cluster. The declared order is preserved.
Missing, deleting, unready, wrong-kind, wrong-parent, cross-namespace, or
duplicate references prevent programming; the controller never skips an
invalid reference to apply a shortened chain. The parent reference is immutable.

The controller deliberately supports **complete-set reordering only**. Before
each apply, it GETs the remote chain and requires the desired IDs to be a complete
permutation of that set, then checks that the PUT response matches the requested
order. It cannot reorder a chain containing policies that cannot be resolved
from its Kubernetes references.
These conservative controller restrictions match the development backend
observations above; they do not depend on every backend version enforcing them.
GET and PUT are not an atomic transaction: avoid concurrent out-of-band changes
to the policy set. No policy-count limit is imposed by this controller; the
backend's actual maximum remains unknown.

The first chain to claim the parent owns it, recorded in the parent annotation
`gateway-operator.konghq.com/cluster-policy-chain-owner`. A competing chain reports
`PolicyChainOwnerValid=False` with reason `ConflictingChain`; it does not write
the remote chain. Delete the owning chain before replacing it.

Reconciliation runs periodically to correct ordering drift. Policy changes and
recreation enqueue referencing chains and invalidate unready references; a
successful chain applies the latest policy IDs on its next configured sync.
If the remote set and declared references differ, `Programmed=False` explains
the complete-set restriction until they match.

Deleting the chain CR releases ownership and stops management. It preserves the
remote chain and all policy objects, even if authentication or referenced policies
are no longer available; it does not PUT an empty list or delete policies.

This controller supports Konnect only. Policy chains are not registered with
the on-prem Event Gateway controller; on-prem ordering and gw-admin application
are outside this layer's scope.
