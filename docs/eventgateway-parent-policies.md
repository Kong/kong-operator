# Event Gateway parent policies

Produce `encryptFields`, `maskFields`, and `transcode` policies and Consume
`decryptFields`, `maskFields`, and `transcode` policies operate on records parsed
by a `schemaValidation` policy. Set
`spec.apiSpec.parentPolicyRef.name` to the Kubernetes name of that parent policy.
The operator resolves the reference to Konnect's `parent_policy_id`.

The parent must have `apiSpec.type: schemaValidation`, be the same kind as the
child (produce or consume), and reference the same virtual cluster in the same
namespace. `parentPolicyRef` uses a namespaced reference with `name`, optional
`kind` (defaulting to the child's kind), and optional `namespace` (defaulting to
the child's namespace). Cross-namespace parent references are rejected during
reference resolution (`KonnectReferencesResolved=False`).

| Child policy type | `parentPolicyRef` |
| ----------------- | ----------------- |
| Produce `encryptFields` | Required |
| Produce `maskFields` or `transcode` | Required |
| Consume `decryptFields` | Required |
| Consume `maskFields` or `transcode` | Required |
| Produce or consume `modifyHeaders` | Optional |
| Consume `skipRecord` | Optional |
| Other policy types | Not supported |

The reference belongs directly under `apiSpec`, not inside the policy variant.
For example, given an `EventGatewayVirtualCluster` named `vc` and an
`EventGatewayStaticKey` named `encryption-key` in the same namespace, both
belonging to the same gateway:

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

New Produce `encryptFields`, `maskFields`, and `transcode` policies and Consume
`decryptFields`, `maskFields`, and `transcode` policies must have a parent reference.
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

A separate probe against `eu.api.konghq.tech` on 2026-10-25 created an isolated
gateway, backend cluster, and fresh virtual cluster with no policies. Before any
chain PUT, three consecutive chain GETs returned `200 {"policies":[]}`. An initial
PUT of `{"policies":[]}` and the subsequent GET also returned `200 {"policies":[]}`.
A chain GET for a nonexistent virtual cluster returned `404`. All temporary
resources were deleted and their absence confirmed. This observation is specific
to the tested development backend: a GET error, including `404`, remains an error
and must not be interpreted as an empty chain or bypass the complete-set guard.

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
The validated, ordered policy IDs are reused by the apply operation within the
same reconciliation, and references are resolved again on the next reconciliation.
Operations invoked without that result resolve and validate the references
themselves; the result is not persisted or cached across reconciliations.

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
Malformed ownership annotations and claims whose owner no longer exists or has
been recreated with a different UID are treated as stale and can be replaced.
Conflicting chains retry at the configured Konnect sync period; parent changes
also enqueue them so ownership can be claimed promptly after release.

Reconciliation runs periodically to correct ordering drift. Policy changes and
recreation enqueue referencing chains and invalidate unready references; a
successful chain applies the latest policy IDs on its next configured sync.
If the remote set and declared references differ, `Programmed=False` explains
the complete-set restriction until they match.

Deleting the chain CR releases ownership and stops management. It preserves the
remote chain and all policy objects, even if authentication or referenced policies
are no longer available; it does not PUT an empty list or delete policies.

### On-prem reconciliation

The same chain API also applies to an `EventGatewayVirtualCluster` whose backend
cluster references an `OnPremEventGateway`. The chain is configuration on that
virtual cluster, not a gw-admin entity. Its policy references resolve to the
policies' Kubernetes UIDs in the complete gw-admin document; neither Konnect
authentication, Konnect IDs nor a policy's previous `Programmed` condition is
required. `status.id` remains unset.

For example, after creating cluster-level policy CRs named `validate-requests`
and `authorize` for the on-prem virtual cluster `vc`, declare their order with:

```yaml
apiVersion: configuration.konghq.com/v1alpha1
kind: EventGatewayVirtualClusterPolicyChain
metadata:
  name: cluster-order
  namespace: default
spec:
  eventGatewayVirtualClusterRef:
    type: namespacedRef
    namespacedRef:
      name: vc
  apiSpec:
    policies:
      - name: validate-requests
      - name: authorize
```

List **every live cluster-level policy CR** for that virtual cluster, in its
namespace, exactly once. An empty list is valid only when there are no such
policies. A policy rejected during configuration assembly does not disappear
from this complete-set requirement and cannot satisfy a reference. Missing,
terminating, duplicate, wrong-kind, wrong-parent or cross-namespace policy
references are invalid; references are never silently skipped.

Ownership uses the same UID-bound parent annotation as Konnect, without claiming
that a local ownership claim means the chain is programmed remotely. An existing
owner is retained across updates and operator restarts. When there is no owner,
the earliest-created eligible chain claims the parent (ties use resource names).
Competing chains report `PolicyChainOwnerValid=False` with reason
`ConflictingChain` and `Programmed=False`; they do not prevent a valid owner from
being applied. Stale claims, including an owner deleted and recreated with another
UID, can be replaced. A terminating owner releases its on-prem claim without
waiting for its finalizers.

An invalid owning declaration reports `Programmed=False` on the chain and
`Programmed=False` / `ConfigurationPushed=False` with reason `InvalidPolicyChain`
on the gateway. **No new document or configuration revision is applied for that
gateway until the declaration is repaired or removed.** This also pauses
unrelated changes and certificate updates on that gateway: gw-admin replaces
the entire document, so neither omitting the virtual cluster nor silently using
default policy ordering would preserve the declared configuration. Already
running pods retain their last document; new or restarted pods cannot be
provisioned from the invalid declaration. GWAdmin access revocation and gateway
deletion still perform their normal cleanup.

If a live virtual cluster is excluded during configuration assembly, its chain
still reports `Programmed=False`, but only its existing UID-bound owner blocks
the gateway. Unclaimed or conflicting declarations do not acquire ownership of
an excluded virtual cluster. Stale and terminating claims are released before
checking whether to block. Deleting the virtual cluster removes its scope from
the document, so its surviving chain does not block updates to the gateway.

Chain changes and policy creation, updates, termination, deletion and same-name
recreation trigger a rebuild. A pure order change changes the configuration hash
and revision and is pushed to gw-admin. Reconciliation also detects and corrects
ordering drift within explicitly managed cluster-policy scopes. Consume and
produce ordering, and comparison behavior for virtual clusters without a chain,
remain unchanged.

Deleting the chain releases its ownership and rebuilds the complete document
using the existing default order: policy creation timestamp, then resource name.
It deletes neither policy CRs nor their entities. Unlike Konnect's retain-on-delete
behavior, on-prem does not preserve the last remote order or cache a historical
ordering. After adding or deleting policy CRs, update the declaration to match;
until then, an incomplete declaration or a reference to a deleted policy pauses
the gateway's configuration updates.

#### Verifying the gw-admin ordering contract

The cluster-level round-trip contract test submits both orders of the same
policies, then repeats the reads to check that gw-admin preserves their order
and payload with its known defaults for omitted optional fields (`condition: ""`,
`description: ""`, `enabled: true`). Comparison accounts for these defaults so
they do not cause repeated pushes. A second test,
`listener_policy_order_survives_an_entities_round_trip`, checks the order of
the policies of a listener only, not their payload. Neither tests consume or
produce ordering.

To run it against a **dedicated real gw-admin**, set `GW_ADMIN_CONTRACT_URL`
(including `/v1`), `GW_ADMIN_CONTRACT_SERVER_NAME`, and the certificate file paths
`GW_ADMIN_CONTRACT_CA`, `GW_ADMIN_CONTRACT_CERT` and `GW_ADMIN_CONTRACT_KEY`, then run:

```bash
go test ./test/envtest/eventgateway -run '^TestGWAdminContract$/^(cluster-level|listener)_policy_order_survives_an_entities_round_trip$' -count=1 -v
```

The tests replace the gateways served by that gw-admin and clean up their
temporary gateway afterward. Without `GW_ADMIN_CONTRACT_URL`, the test uses the
fake backend; passing against the fake does not establish the real backend's
ordering contract.

On 2026-11-04, the cluster-level test passed against the pinned
`kong/kong-gw-admin-cp-dev:pr-1502` image
(`sha256:eb9db0feab9291065ba185f6fb5841661f39ce93868c311346ae3599d8d0522b`).
That backend preserved both submitted orders across repeated reads and supplied
the three defaults listed above. This observation applies to the tested image,
not every gw-admin version. The listener test hasn't been run against a real
gw-admin.

## Listener policy-chain API

`configuration.konghq.com/v1alpha1` `EventGatewayListenerPolicyChain` declares the
execution order of all `EventGatewayListenerPolicy` resources attached to one
`EventGatewayListener`. The Konnect controller manages this parent-scoped
singleton configuration for a listener of a `KonnectEventGateway`, the on-prem
Event Gateway controller for a listener of an `OnPremEventGateway` (see
[On-prem listener-chain reconciliation](#on-prem-listener-chain-reconciliation)).

`spec.eventGatewayListenerRef` must use `namespacedRef`. A listener of another
namespace needs a `KongReferenceGrant` there allowing the
`EventGatewayListenerPolicyChain` to reference it. The parent reference is immutable from
creation, even before programming; create a new chain CR to manage a different listener.
The chain configures its parent rather than creating an independent Konnect
entity, so `status.id` must remain unset. The shared status structure provides
conditions, organization/server information, and parent IDs instead.

Supply `spec`, `spec.apiSpec`, and `spec.apiSpec.policies` explicitly. `policies`
is an atomic ordered list of Kubernetes references, not Konnect IDs. Each entry
has a required `name`, an optional `kind` defaulting to
`EventGatewayListenerPolicy`, and an optional `namespace` defaulting to the
chain's namespace. A policy of another namespace needs a `KongReferenceGrant`
there allowing the `EventGatewayListenerPolicyChain` to reference it. Every
policy must belong to the referenced listener. List order, not resource names
or YAML document order, declares execution order. See the
[listener-chain example](../config/samples/konnect_eventgateway_listener_policy_chain.yaml).
It includes API authentication, the gateway, listener, backend, virtual cluster,
forwarding policy, and chain in one file. Replace the token and Kafka/data-plane
addresses before applying it. Its plaintext, anonymous port-mapping setup is
for demonstration only.

The declaration models complete-set reordering: list every existing listener
policy exactly once. An explicit `policies: []` is valid only when the listener
has no policies; it does not clear an existing chain. Omitted and null lists
are invalid. New policies append to the remote chain, deletion preserves the
remaining order, and recreation gives the policy a new Konnect ID. Update the
declared list when policies are created or deleted.

Independent validation against the development backend `eu.api.konghq.tech`
on 2026-11-07 confirmed that a fresh listener's chain GET returns
`200 {"policies":[]}`, and PUT accepts an empty list only for an empty scope.
Complete permutations are accepted and returned in the requested order.
Partial, duplicate, unknown, wrong-parent, missing, and null policy lists are
rejected with `400` without changing the remote order. These are observations
from the tested development backend, not a guarantee for every production
version. The pinned SDK already provides listener-chain GET and PUT operations;
no SDK upgrade is required for this API.

### Konnect listener-chain reconciliation

Each reference must resolve to a distinct policy on the same listener and gateway,
with a nonempty Konnect ID and `Programmed=True` at its current generation.
Missing, deleting, unready, wrong-kind, wrong-parent and duplicate references
block programming with an actionable `KonnectReferencesResolved=False`
condition, and so does a policy of another namespace no `KongReferenceGrant`
allows, with reason `ReferenceNotPermitted`. Without a `KongReferenceGrant` for
a listener of another namespace, the chain reports `ResolvedRefs=False` with
reason `RefNotPermitted`, doesn't claim the listener, and isn't applied. The
controller reads each policy once per reconciliation and reuses that snapshot's
ordered IDs for the apply operation; it resolves references anew
on the next reconciliation. It never silently skips an invalid policy.

Before applying, the controller GETs the listener chain and requires the declared
IDs to be an exact complete-set permutation of the remote IDs. Empty IDs,
duplicates, omitted or null remote lists, nil responses, and GET errors (including
`404`) are failures, not an empty chain. It PUTs the requested order only after
this check and verifies that the response contains exactly that order. A
policy-set mismatch keeps `Programmed=False` until the declaration and remote set
align. GET and PUT are not atomic; avoid concurrent out-of-band policy-set changes.

The first chain to claim the listener owns it, using the UID-bound annotation
`gateway-operator.konghq.com/listener-policy-chain-owner` on that listener.
This is independent of cluster-chain ownership. Optimistic-lock patches serialize
competing declarations, and ownership reads bypass the Kubernetes cache so an
active owner cannot be mistaken for a stale one. Another chain reports
`PolicyChainOwnerValid=False` with reason `ConflictingChain` and does not write
Konnect. Malformed, missing-owner,
recreated-owner, and wrong-parent claims can be reclaimed. Delete the owner before
replacing it; a terminating owner retains its claim until cleanup releases it.

Periodic reconciliation corrects ordering drift. Policy spec/status changes,
deletion, and recreation enqueue referencing chains, as do listener changes.
Ownership conflicts retry at the configured Konnect sync period; listener changes
also allow prompt handover after release.

Deleting the chain CR releases ownership and stops management while preserving
remote policy order and all policy entities. Cleanup uses Kubernetes only and
does not require working authentication or resolvable policy references. It does
not PUT an empty list or call a remote DELETE endpoint.

### On-prem listener-chain reconciliation

A chain whose listener references an `OnPremEventGateway` orders the policies
of the listener in the complete gw-admin document, as a cluster policy chain
orders those of its virtual cluster (see [On-prem reconciliation](#on-prem-reconciliation)):
its references resolve to the policies' Kubernetes UIDs, without Konnect, and
`status.id` remains unset.

List every live policy of the listener exactly once. Missing, terminating,
duplicate, wrong-kind and wrong-parent references, and a policy left out of
the configuration, are invalid. A listener or policy of another namespace needs
a `KongReferenceGrant` there: without one for its listener, a chain is left
out without holding back the gateway; without one for a policy, it reports
`ReferenceNotPermitted` and, when no other chain owns the listener, holds back
the gateway, as it would keep the default order of the policies.

Ownership uses the same annotation as Konnect,
`gateway-operator.konghq.com/listener-policy-chain-owner`, independent of
cluster-chain ownership: the earliest-created eligible chain claims the
listener, a competing chain reports `PolicyChainOwnerValid=False` with reason
`ConflictingChain`, and a terminating owner releases its claim without waiting
for its finalizers.

An invalid owning chain reports `Programmed=False`, and the gateway
`Programmed=False` / `ConfigurationPushed=False` with reason
`InvalidPolicyChain`: no new document is applied to the gateway until the chain
is repaired or removed. Reconciliation corrects drift of the order of the
policies of a listener a chain owns. Deleting the chain releases its claim and
rebuilds the document with the default order of the policies, creation
timestamp then resource name, deleting neither policies nor their entities.

## Produce policy chains (Konnect only)

`configuration.konghq.com/v1alpha1` `EventGatewayVirtualClusterProducePolicyChain`
declares the execution order of all `EventGatewayVirtualClusterProducePolicy`
resources in one virtual cluster. The Konnect controller resolves the declaration
and reorders the complete remote chain without creating or deleting policies.
This is Konnect management-plane ordering, not a Kafka execution or traffic-routing
guarantee. Consume ordering is separate, and on-prem Produce chain support is not
included or registered.

`spec.eventGatewayVirtualClusterRef` must use `namespacedRef` and reference an
`EventGatewayVirtualCluster` in the chain's namespace. The reference is immutable
from creation, even before programming. An omitted or empty namespace defaults
locally; an explicit namespace must be the same as the chain's, regardless of
`KongReferenceGrant` permissions. The chain is singleton configuration on that
parent, not an independent Konnect entity: `status.id` must remain unset and
there is no chain-ID printer column. Shared status retains conditions,
organization/server information, `gatewayID`, and `virtualClusterID`.

Supply `spec`, `spec.apiSpec`, and `spec.apiSpec.policies` explicitly.
`policies` is one atomic ordered list of Kubernetes references, not Konnect IDs.
Each entry requires `name`; `kind` defaults to
`EventGatewayVirtualClusterProducePolicy`, and an omitted or empty `namespace`
defaults to the chain's namespace. An explicit namespace must be that same
namespace. Every policy must belong to the referenced virtual cluster.
Resource names, YAML document order, and creation order do not declare execution
order. See the [self-contained Produce chain example](../config/samples/konnect_eventgateway_virtualcluster_produce_policy_chain.yaml),
which includes placeholder authentication, gateway, backend, virtual cluster,
a schema-validation parent, two nested header policies, a separate root policy,
and their complete ordering declaration. Replace the token and Kafka bootstrap
address before applying. Anonymous authentication and disabled TLS are for
demonstration only.

### Complete-set and nested ordering contract

The pinned SDK v0.82.2 provides GET and PUT at
`/v1/event-gateways/{gatewayId}/virtual-clusters/{virtualClusterId}/produce-policy-chain`.
The shared `UpdateEventGatewayPolicyChainRequest` body contains `policies: []string`.
There is no independent chain ID, POST, list-for-UID, or DELETE operation.
Development-backend observations from `eu.api.konghq.tech` on 2026-11-07 established
the following contract; they do not guarantee behavior for every production
version or region.

- GET returns a single flat ordered list containing **all roots and nested
  children**. PUT must contain every existing Produce policy exactly once.
  Complete-set permutations are accepted; partial, duplicate, unknown, foreign,
  omitted, and null lists are rejected without changing the order.
- An explicit `policies: []` is valid for an already-empty scope only. It never
  clears existing policies. New policies append, deletion preserves the order
  of surviving policies, and recreation produces a new policy ID.
- A parent precedes its children, and each parent's group remains contiguous.
  Root/parent groups may move, and sibling children may reorder within their
  group. Child-before-parent order, roots inserted between a parent and its
  children, cross-parent interleaving, roots-only lists, and children-only lists
  are invalid.
- Ordering does not change `parent_policy_id`. Existing `parentPolicyRef`
  validation and immutability remain unchanged: Produce `encryptFields`,
  `maskFields`, and `transcode` require a same-kind, same-namespace,
  same-virtual-cluster `schemaValidation` parent; `modifyHeaders` may reference
  one. Moving a child to another parent requires recreating the policy, not
  moving its chain entry.

No separate nested-list declaration is needed: the complete atomic list
represents both root-group and sibling order without changing parent references.
Update it when policies are created or deleted. The example's
`schemaValidation.config.json.valueValidationAction: reject` is explicit because
the observed backend requires a validation action despite incomplete OAS
requiredness.

### Reconciliation and lifecycle

Admission validates the declaration's shape, kinds, immutable parent reference,
and absent independent ID; it cannot resolve policy resources or remote sets.
The controller rejects missing, deleting, unready, stale-generation,
wrong-kind, cross-namespace, wrong-virtual-cluster, duplicate, and invalid
parent/group references without applying a shortened chain. Parent namespace
validation happens before lookup or adoption of parent IDs, authentication,
grant checks, or ownership. Reference resolution captures one snapshot per policy
per reconciliation, and nested validation and programming reuse that complete
ordered set of IDs. A child's included parent must be a ready root
`schemaValidation` policy; cycles and deeper nesting are rejected. The controller
never sorts declarations or rewrites `parentPolicyRef` to make them valid.

`KonnectReferencesResolved=False` distinguishes invalid declarations from missing
or not-yet-programmed dependencies. Such failures also block `Programmed=True`.
Ownership uses the virtual cluster's
`gateway-operator.konghq.com/produce-policy-chain-owner` annotation, bound to the
chain's name and UID, independently of cluster, listener, or Consume scopes.
Optimistic locking and live ownership reads prevent active owners from being
displaced by cache lag. Conflicts report `PolicyChainOwnerValid=False` with
`ConflictingChain`; stale or malformed claims can be reclaimed.

Policy spec/status, deletion, recreation, and virtual-cluster changes enqueue
chains. Produce policy changes also enqueue chains sharing the virtual cluster
even if they do not yet reference that policy. Parent-policy dependencies are
validated from the same included policy snapshots. Periodic reconciliation
corrects remote ordering drift; recreation resolves the current policy IDs.
Watch-triggered reconciliations check the remote set even within the sync period,
without repeating PUT or changing programming timestamps when the order already
matches.
Update the declaration when the complete policy set changes.

Deleting a declaration releases only its matching claim and cleanup finalizer,
retaining remote ordering and policy entities. Cleanup does not require working
authentication, a ready parent, or resolvable policy references, and never PUTs an
empty list or calls a remote DELETE endpoint. Comprehensive lifecycle and
real-Konnect Chainsaw coverage is a separate final layer of
[#6368](https://github.com/Kong/kong-operator/issues/6368).

GET and PUT are not atomic against concurrent external policy-set changes.
A GET failure, including `404`, must remain an error rather than an empty chain;
a changed or incomplete set blocks programming until the declaration and remote
set agree. A successful PUT response must return exactly the requested order.

## Consume policy-chain API (Konnect only)

`configuration.konghq.com/v1alpha1` `EventGatewayVirtualClusterConsumePolicyChain`
declares the execution order of all `EventGatewayVirtualClusterConsumePolicy`
resources in one virtual cluster. This layer supplies the API, CRD, Helm
installation artifacts, and admission contract only. Consume-chain controller
registration and runtime reconciliation are not included yet. This is a
Konnect management-plane declaration, not on-prem Consume-chain support or a
guarantee about Kafka data-path execution.

`spec.eventGatewayVirtualClusterRef` must use `namespacedRef` for a virtual
cluster in the chain's namespace and is immutable from creation, even before
programming. An omitted or empty namespace defaults locally; an explicit
namespace must be the same as the chain's, even if a `KongReferenceGrant` permits
cross-namespace access. The chain is singleton configuration on that parent:
there is no independent chain ID or chain-ID printer column. `status.id` must
remain unset; shared status retains conditions, organization/server information,
`gatewayID`, and `virtualClusterID`.

Supply `spec`, `spec.apiSpec`, and `spec.apiSpec.policies` explicitly.
`policies` is one atomic ordered list of Kubernetes references, not Konnect IDs.
Each entry requires a distinct `name`; `kind` defaults to and must be
`EventGatewayVirtualClusterConsumePolicy`. An omitted or empty `namespace`
defaults to the chain's namespace, and an explicit namespace must match it.
Every policy must belong to the referenced virtual cluster. Resource names,
creation order, and YAML document order do not declare execution order.

### Complete-set and nested Consume ordering

The pinned SDK v0.82.2 supplies `GetEventGatewayVirtualClusterConsumePolicyChain`
and `UpdateEventGatewayVirtualClusterConsumePolicyChain` at
`/v1/event-gateways/{gatewayId}/virtual-clusters/{virtualClusterId}/consume-policy-chain`.
The shared `UpdateEventGatewayPolicyChainRequest` contains `policies: []string`.
There is no independent chain POST, list-for-UID, or DELETE operation.

The declaration must include every root and nested Consume policy exactly once
in one flat list. An explicit `policies: []` is valid only for an already-empty
remote scope; it is not a request to clear policies. Omitted and null lists are
invalid. Additions and recreations append their new remote IDs; deletion removes
only that ID. Update the declaration whenever the complete policy set changes.

A root `schemaValidation` parent precedes its immediately contiguous children.
Whole root/parent groups may move, and siblings may reorder within their group.
Children cannot precede their parent, interleave another parent's children, or
be separated from their parent by a root policy. Roots-only and children-only
declarations are incomplete. Only root parents and their direct children are
supported; cycles and deeper nesting are outside this contract.

Ordering never changes `parentPolicyRef` or reparents policies. Current Consume
policy validation is unchanged: `decryptFields`, `maskFields`, and `transcode`
require a same-kind, same-namespace, same-virtual-cluster `schemaValidation`
parent. `modifyHeaders` and `skipRecord` may reference one; no other Consume
variant accepts `parentPolicyRef`. Once the policy exists in Konnect, its parent
reference cannot be added, changed, or removed. Recreate a child to change its
parent.

The [self-contained Consume example](../config/samples/konnect_eventgateway_virtualcluster_consume_policy_chain.yaml)
includes placeholder authentication, gateway, backend, virtual cluster, two
schema-validation parents, nested header/decryption/skip policies, a separate
root, and the complete group-aware ordering. Its explicit
`schemaValidation.config.json.valueValidationAction: skip` is needed for the
observed Consume backend contract, despite incomplete OAS requiredness.
Replace the token and Kafka bootstrap address before use; anonymous
authentication and disabled TLS are demonstration settings only.

Consume-specific development-backend probes confirmed singleton GET/PUT,
complete-set permutations, and empty-scope GET/PUT with `[]`. Nonempty-scope
empty lists, incomplete lists, duplicates, unknown IDs, wrong-parent IDs,
missing/null lists, child-before-parent order, split groups, and cross-parent
interleaving were rejected with `400` without changing the order. Reordering
siblings and moving whole groups preserved `parent_policy_id`. The nested probe
covered two schema-validation parents with `modifyHeaders`, `decryptFields`,
and `skipRecord` children; it did not establish backend behavior for nested
`maskFields` or `transcode`. These observations do not verify Operator
reconciliation or guarantee behavior in every backend version or region.

Admission validates required fields, reference shape/kind, immutable parent
reference and `namespacedRef` form, and absence of an independent ID. It cannot
resolve other resources, enforce same-namespace or distinct-reference semantics,
verify parent groups, or check the remote complete set. Those checks belong to
the subsequent controller layer; this API-only layer does not apply the declaration.

### Consume controller and lifecycle coverage

The runtime layer above that API change registers Konnect Consume-chain
reconciliation. It resolves every declared policy once at its latest observed
generation, validates its virtual-cluster/gateway ancestry and contiguous parent
group, and reuses the full ordered snapshot for GET-before-PUT complete-set
checks. A malformed response, unresolved reference or incomplete set cannot
produce a reduced successful chain. An already matching order requires no PUT.

Ownership uses `gateway-operator.konghq.com/consume-policy-chain-owner`, bound to
the chain's name and UID and independent of cluster and Produce claims on the
same virtual cluster. Resource-version guarded claims and live owner reads
protect competing declarations even when the informer cache lags. Deleting a
declaration releases only its own claim and retains the remote order and policy
entities, without requiring working authentication or valid references.

The [Consume lifecycle scenario](../test/e2e/chainsaw/eventgateway/README.md#consume-policy-chain-lifecycle)
covers two real parent groups, exact full ordering and parent IDs, drift,
reference/set blocking, recreation, ownership handover and retained deletion.
Local helper and API-server regressions are not a substitute for running this
scenario against an authorized dedicated Konnect scope. Until that acceptance
run is recorded, no full Operator lifecycle acceptance is implied by the earlier
backend contract probes. `maskFields` and `transcode` retain typed/admission and
mocked-controller coverage only; this scenario does not establish their live
backend contract. The Konnect-only, direct-child and non-atomic GET/PUT boundaries
remain unchanged.
