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

## Two-Layer Design

Hashing in `controller/konnect/ops/configversion_hash.go` is split into two layers:

1. **Canonical JSON encoding** (`canonicalPayloadJSON`, `canonicalRepresentationJSON`): turns strongly-typed SDK request/response structs into deterministic canonical JSON bytes.
2. **Digest computation** (`entityPayloadHash`, `representationHash`): hashes the canonical bytes (currently `sha256:<hex>`).
   Keyed HMAC digests for secret-bearing payloads can plug into this digest layer without changing the canonicalization rules or the SHA-256 golden hashes of other entity types.
   The keyed digests of the affected types are new values, which need their own golden hashes and must be told apart from SHA-256 digests in `status.sync`, for example by a distinct prefix.

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
