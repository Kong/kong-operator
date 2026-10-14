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
