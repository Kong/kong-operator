# Konnect Sync Hashing Contract

This document defines the canonicalization, hashing, SDK-coupling, and `hashVersion` migration contract for `status.sync` (`configurationv1alpha1.SyncStatus`) used by `ConfigVersionTracker` in `controller/konnect/ops/`.

## Scope

The sync state guard (`upsertUnlessInSync`) covers the 17 Kong Gateway configuration entity types:

- `KongService`
- `KongRoute` (`RouteJSON` and `RouteExpression`)
- `KongPluginBinding`
- `KongConsumer` (request plus resolved consumer group IDs and invalid group refs)
- `KongConsumerGroup`
- `KongUpstream`
- `KongTarget`
- `KongCACertificate`
- `KongCertificate`
- `KongSNI`
- `KongKey`
- `KongKeySet`
- `KongCredentialBasicAuth`
- `KongCredentialAPIKey`
- `KongCredentialACL`
- `KongCredentialJWT`
- `KongCredentialHMAC`

It also covers the Konnect AI Gateway entity types whose generated update opts into `ops.skipUnchangedUpdates`. Their sync state is tracked in memory only.
See [AI Gateway Entities](#ai-gateway-entities).

## Two-Layer Design

Hashing in `controller/konnect/ops/configversion_hash.go` is split into two layers:

1. **Canonical JSON encoding** (`canonicalPayloadJSON`, `canonicalRepresentationJSON`): turns strongly-typed SDK request/response structs into deterministic canonical JSON bytes.
2. **Digest computation** (`syncHasher`, `entityPayloadHash`, `representationHash`): hashes the canonical bytes, either as an unkeyed SHA-256 digest (`sha256:<hex>`) or, for secret-bearing entities, as a keyed HMAC-SHA256 digest (`hmac-sha256:<hex>`).
   See [Keyed HMAC-SHA256 Hashes](#keyed-hmac-sha256-hashes). Both digests use the same canonical bytes.

## Canonicalization Rules

Both desired payloads and Konnect representations go through JSON serialization followed by `json.Decoder` with `UseNumber()` and recursive canonicalization:

- **Recursive key sorting**: Every JSON object at every depth (including Go structs, SDK unions, nested maps such as `Plugin.Config`, and header maps) is decoded into `map[string]any` and re-encoded with `encoding/json`, which sorts object keys lexicographically.
- **Exact numeric preservation**: `json.Decoder.UseNumber()` preserves exact JSON number tokens so large `int64` values do not lose precision via `float64`.
- **Arrays keep their order**: No array is reordered, at any depth. A different order means a different payload or representation:
  - Payloads are built deterministically, so equal desired states produce equal arrays. For example, `GenerateTagsForObject` sorts tags, and `consumerSyncPayload` sorts the resolved consumer group IDs and invalid references.
  - Representations are compared with representations only, and Konnect returns arrays in the same order on PUT and GET (see below).
- **Volatile representation fields**: When hashing Konnect entity representations, the fields listed in `volatileRepresentationFields` (`created_at`, `updated_at`) are removed from the top-level object only.
  - They are server-managed fields of the entity itself. `TestRepresentationTypesHaveTopLevelTimestampsOnly` checks that no guarded representation type has them below the top level.
  - A nested key with the same name is user data, for example a route header, a plugin `config` field, or a label, and is kept so that a change to it is detected.
  - A field missing from the list costs a PUT whenever the entity is verified with a GET, not a missed change.

### Verified Konnect PUT vs GET Behavior

Empirical PUT vs GET checks against Konnect across all 17 entity types confirm:

- After stripping `created_at` and `updated_at`, PUT and GET responses have identical canonical representations for all 17 entity types. No other field changes between them.
- Konnect normalizes some arrays on write, for example `protocols` sent as `["https","http"]` is stored as `["http","https"]`, and returns the normalized order on both PUT and GET.
  Other arrays, such as `tags` and `paths`, keep the request order on both. Hashes never compare a request with a response, so arrays need no sorting.
- `KeyAuth.ttl` is a live server-side countdown when set (for example `3600` on PUT, `3598` on GET two seconds later).
  Because `KongCredentialAPIKeySpec` does not expose `ttl`, operator-managed API keys omit `ttl` on both PUT and GET and match cleanly.
  Keeping `ttl` hashed ensures external TTL modifications in Konnect are detected as drift.
  If `KongCredentialAPIKeySpec` adds `ttl` support in the future, `KeyAuth.ttl` must be excluded or handled separately in representation canonicalization, accompanied by a `SyncHashVersion` bump.

## SDK-Coupling Contract and Golden Hash Tests

Payload and representation hashes are computed from the serialized JSON of `github.com/Kong/sdk-konnect-go` request and response types. This deliberately couples persisted `status.sync` digests to SDK JSON serialization (field names, `default` tags, `omitempty` behavior, and union marshaling).

To prevent silent hash drift during `sdk-konnect-go` upgrades or canonicalization refactors, `TestEntitySyncHashesPerType` in `controller/konnect/ops/configversion_tracker_entity_hashes_test.go` pins exact golden SHA-256 hashes for all 17 entity types (including both `RouteJSON` and `RouteExpression`).
It also pins keyed HMAC-SHA256 golden hashes, computed with a fixed test key, for the secret-bearing entity types (`KongPluginBinding` and `KongCertificate`), and fails when an entity type's secret-bearing classification does not match its golden fixtures.

When `TestEntitySyncHashesPerType` fails in CI:

1. Identify why the canonical JSON changed (for example an `sdk-konnect-go` default change, new serialized field, or canonicalization update).
2. Never update the golden hashes silently for a released `SyncHashVersion` without assessing the upgrade impact on existing clusters.
3. If the change alters hashes produced by an already-released `SyncHashVersion`, follow the `hashVersion` migration policy below and update the golden hashes in the same PR.

## `hashVersion` Migration Contract

`SyncHashVersion` (in `controller/konnect/ops/configversion_hash.go`) is stored alongside `payloadHash` and `representationHash` in `status.sync.hashVersion`.

- **No cross-version comparison**: `syncGuard.recordFromStatus()` rejects any persisted `SyncStatus` whose `HashVersion != SyncHashVersion`. Hashes produced by different schemes are never compared directly.
- **Default migration path (documented one-time re-sync)**: When `SyncHashVersion` is bumped (for example `"v1"` -> `"v2"`), each existing entity's persisted status is treated as unverified on the first reconcile after upgrade.
  The entity is sent to Konnect once via PUT and updated in `status.sync` with the new `SyncHashVersion` and digests.
  Subsequent reconciles return to skipping unchanged entities and GET-verifying across restarts.
- **When to consider N-1 dual-verification**: If a future breaking hash change would cause an unacceptable one-time PUT burst on large control planes, retain the N-1 canonicalizer/hasher for one release.
  GET-verify existing entities with the N-1 scheme first, then rewrite `status.sync` to the new `SyncHashVersion` without sending a PUT.
- **Release notes**: Any `SyncHashVersion` bump between releases must be noted in `CHANGELOG.md` so operators know to expect a one-time re-sync on upgrade.

## Keyed HMAC-SHA256 Hashes

`status.sync` is readable by anyone who can read the entity, which is often a wider audience than the one that can read the Secrets the entity is built from.
An unkeyed SHA-256 digest of a payload that contains data read from a Secret lets such a reader test guesses of that data offline, which is practical for low-entropy secrets such as passwords.
The sync hashes of secret-bearing entities are therefore keyed HMAC-SHA256 digests computed with an operator-managed key.

### Secret-Bearing Entity Types

An entity type is secret-bearing when its Konnect payload can carry secret material read from a Secret that is not readable from the entity itself.
Only then does a keyed digest protect anything: a hash cannot expose more than the entity it is stored in.
`isSecretBearingSyncEntity` in `controller/konnect/ops/configversion_hmac.go` lists these types:

- `KongPluginBinding`: plugin configuration can be sourced from Secrets through the `KongPlugin` `configFrom` and `configPatches` fields. Neither the binding nor the `KongPlugin` contains these values.
- `KongCertificate`: the private key can be read from a Secret through `secretRef` and `secretRefAlt`. Inline certificates are keyed too, so the hash kind of an entity does not depend on its source type.

The other guarded types keep unkeyed SHA-256 digests:

- `KongCredentialBasicAuth`, `KongCredentialAPIKey`, `KongCredentialJWT`, `KongCredentialHMAC`, and `KongKey` carry secrets, but in their own spec. Credentials created from `KongConsumer` credential Secrets get the Secret data copied into their spec as well.
  Anyone who can read their `status.sync` can read the secret itself, so a keyed digest adds no protection. If one of these types gains a reference to a Secret, it becomes secret-bearing.
- `KongCACertificate`: a CA certificate is public even when it is read from a Secret.

When a new entity type is guarded by `upsertUnlessInSync`, decide whether it is secret-bearing and add the keyed golden hashes to `TestEntitySyncHashesPerType` if it is.

### Status Format

- `payloadHash` and `representationHash` use the `hmac-sha256:` prefix instead of `sha256:`. The prefix is part of the comparison: a keyed and an unkeyed digest are never compared.
- `keyID` identifies the key: the first 16 hex characters of `HMAC-SHA256(key, "kong-operator/konnect-sync-hash-key-id")`. It does not reveal the key. It is empty for unkeyed digests.
- `hashVersion` keeps identifying the canonicalization scheme only. The key is identified by `keyID`.

`syncGuard.recordFromStatus()` only accepts persisted hashes whose kind matches the entity type:
unkeyed hashes without `keyID` for unkeyed types, and keyed hashes whose `keyID` is the current or the previous key for secret-bearing types.
Anything else is unverifiable and the entity is sent to Konnect once more.

### Key Secret

The key is stored in an operator-managed Secret in the operator namespace, named by `--konnect-sync-hash-key-secret` (default `kong-operator-konnect-sync-hash-key`, environment variable `KONG_OPERATOR_KONNECT_SYNC_HASH_KEY_SECRET`):

| Data key      | Required | Description                                                                         |
|---------------|----------|-------------------------------------------------------------------------------------|
| `key`         | Yes      | The current key, at least 32 bytes. New hashes are computed with it.                |
| `previousKey` | No       | The key that was current before a rotation, at least 32 bytes. Used for migration. |

- `EnsureKonnectSyncHashKeys` in `modules/manager/konnect_sync_hash_key.go` reads the Secret before the controllers start, so the first reconcile after a restart can already verify persisted hashes.
  When the Secret does not exist, it creates one with 32 random bytes in `key`. Replicas racing to create it handle `AlreadyExists` by reading the Secret created first.
- An existing Secret is never overwritten. An invalid `key` is an error, and an invalid `previousKey` is ignored with an error log.
- The keys are read once at startup. A change to the Secret takes effect when the operator restarts.
- The created Secret carries the `--secret-label-selector` label, like the cluster CA Secret.
- RBAC: the operator already has cluster-wide `get` and `create` permissions on Secrets, so neither the generated RBAC nor the Helm chart needs a change. The chart does not manage the Secret, so `helm uninstall` keeps it and a reinstall reuses it.
- The key and the raw payloads are never logged or persisted. Logs only carry the key IDs.

### Key Unavailable

When the Secret cannot be read or created, for example because RBAC was restricted, the operator logs an error and still starts.
Secret-bearing entities then fall back to the in-memory tracker with unkeyed digests that never leave the process:
a real PUT clears `status.sync` instead of persisting unkeyed hashes, and the entities are sent to Konnect once after every restart.
Unkeyed entity types are not affected.

### Key Rotation

Rotation is explicit and needs a restart of every operator replica, for example with `kubectl rollout restart`:

- **Without a PUT burst (recommended)**: move the current `key` to `previousKey`, write 32 new random bytes to `key`, and restart the operator.
  On the first periodic reconcile, each secret-bearing entity whose `keyID` is the previous key is compared using the previous key and verified with a GET.
  When it is in sync, its hashes are rewritten with the current key without a PUT. This is a documented explicit verification status write, one per entity.
  Entities that changed in Kubernetes or drifted in Konnect are sent. Remove `previousKey` once the entities were reconciled, at the latest after the drift check period.
- **With a one-time re-sync**: replace `key`, or delete the Secret so the operator creates a new one. Every secret-bearing entity is sent to Konnect once, and its status is rewritten with the new key.

Restoring entities from a backup without the key Secret, or with a different one, behaves like the one-time re-sync.

## AI Gateway Entities

Konnect AI Gateway entities (`aiconfiguration.konghq.com`) skip unchanged periodic updates like Kong Gateway entities, with the same guard (`upsertUnlessInSync`) and canonicalization.

### Config Scopes

`ConfigVersionTracker` tracks the expected config version per config scope:

- A Kong Gateway control plane, keyed by its ID, read with `GET /v2/control-planes/{id}/expected-config-version`.
- An AI Gateway, keyed by `sdkops.AIGatewayWriteScope` (`ai-gateways/<id>`), read with `GET /v1/ai-gateways/{id}/expected-config-version`.

A no-op write does not bump either version, and a change does. This was verified against Konnect for both scopes.
The write observer transport reports writes to `/v2/control-planes/{id}` and `/v1/ai-gateways/{id}`, and the paths below them, to the matching scope. The operator's own writes are therefore not mistaken for external changes.

### Generated Guard

`ops.skipUnchangedUpdates` in `crd-from-oas/config.yaml` makes the generated `update<Entity>` function take the sync guard from `UpdateGeneratedOps`:

- The payload is the SDK update request, with the Kubernetes metadata labels.
- The GET-verification reads the entity with its GET-by-ID operation. The parser extracts it from the item path.
- The generator refuses the option unless the entity has a single `KonnectAIGateway` parent, a GET-by-ID operation, GET and update operations returning the same schema, and no `ops.responseStatusFields`.
- `generatedAIGatewaySyncScope` tells `ops.Update` which entities are guarded, and the ID of their AI Gateway.

### Associations

An entity with `associations`, such as the consumer groups of `AIGatewayConsumer`, needs two more hand-written helpers per association:

- `syncState<Entity><Field>` returns the resolved association state, which is part of the payload (`syncPayloadWithAssociations`). The entity is sent again when the association, or the validity of a reference, changes.
  When the state cannot be resolved, the update is not guarded and always sends.
- `enforce<Entity><Field>IfDrifted` is called when the entity was verified with a GET. It reads the association in Konnect and only writes it when it drifted.
  A skipped update does not enforce the association. A sent update enforces it like before.

### Persistence

The sync state of AI Gateway entities is not persisted in status. After a restart or a leader change, each entity is sent once more.
Because the state never crosses operator versions, there are no golden hashes for these types and no `hashVersion` contract yet. Persisting the state would require both.

