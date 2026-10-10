# Engineering brief: AI Gateway 2.0+ on-prem support (dbless) in kong-operator, push config approach

| | |
| --- | --- |
| Authors | Patryk Małek |
| Status | Sep 3, 2026 — Draft |
| Related materials | [Configuring on-prem AI Gateway 2.0 data planes: push vs pull](https://docs.google.com/document/d/1rGbKfitXP1cHrhlB66NfCGw1HsepXf_VqfXaOz7Dm7w/edit?tab=t.0) (RFC that this brief follows up on) · [Kong/kong-operator#5402](https://github.com/Kong/kong-operator/issues/5402) |

## Goals

- Engineering overview of AI Gateway 2.0+ on-prem (dbless) support in kong-operator, using the
  push-config approach already proven for API Gateway.
- High-level sketch of the on-prem AI Gateway control-plane CRD, referenced by the
  `aiconfiguration` entity CRDs as the second option next to `KonnectAIGateway` (shipped in 2.3).
- Name what is and is not reusable from the ingress-controller config-push pipeline, with the
  hard constraints that decide it.

## Non-goals

- db-backed AI Gateway 2.0+ deployments.
- Full CRD specs. Field-level design is deferred.
- Telemetry stream configuration (`KONG_CLUSTER_TELEMETRY_*` has no on-prem equivalent; needs its
  own design).
- The AI Gateway runtime `config` field on `AIGatewayDataPlane` ([#4840](https://github.com/Kong/kong-operator/issues/4840)).

## Context

The [push-vs-pull RFC](https://docs.google.com/document/d/1rGbKfitXP1cHrhlB66NfCGw1HsepXf_VqfXaOz7Dm7w/edit?tab=t.0)
settled on **Option A: push, the KIC way** — the operator discovers each AI Gateway data-plane
pod's Admin API and actively pushes configuration to it, the way KIC has always configured classic
Kong Gateway. This brief is the engineering design for that: a new `OnPremAIGateway`
control-plane CRD, translation of the existing `aiconfiguration` entity CRDs into
ai-deck-converter's entity model, and a `POST /config` push loop. dbless only; db-backed is out of
scope.

Prior art that already exists in-tree:

| Piece | Where | State |
| --- | --- | --- |
| `AIGatewayDataPlane` CRD + controller | `api/aigateway/v1alpha1`, `controller/aigateway/dataplane/` | Owns Deployment/Service/HPA/Secret. Konnect pull only. **No admin listener.** |
| 10 AI config entity CRDs | `api/aiconfiguration/v1alpha1` (OAS-generated) | Konnect-only, via the generic Konnect reconciler |
| `KonnectAIGateway` | `api/konnect/v1alpha1/zz_generated_konnect_aigateway_types.go` | The Konnect flavour of the control plane |
| Config push machinery | `ingress-controller/internal/{dataplane,adminapi,clients}` | Production-proven for classic Gateway |
| `ai-deck-converter` | `github.com/Kong/ai-deck-converter` | Emits the finished dbless payload. **Not yet a dependency.** |

`ai-deck-converter` is the pivot. It already turns an AI Gateway entity-model document into
exactly the flattened dbless declarative payload a Kong data plane accepts:

```go
// github.com/Kong/ai-deck-converter/convert
func ConvertDocumentToDBLessYAML(doc *aigw.Document, opts Options) ([]byte, []string, error)

type Options struct {
    Strict               bool    // unresolved refs become errors
    LabelTagPrefix       string  // prefix for label-derived tags
    OutputMode           string  // "deck" | "db-less"
    ModelSelectorSources *bool   // ai-model-selector schema: config.sources vs legacy config.source
}
```

`aigw.Document` is the input envelope, one key per entity kind
(`ai-deck-converter/internal/aigw/doc.go`, re-exported via `aigw/doc.go` type aliases):

```go
type Document struct {
    Models         []Model         `yaml:"models,omitempty"`
    ModelProviders []Provider      `yaml:"model_providers,omitempty"`
    MCPServers     []MCPServer     `yaml:"mcp_servers,omitempty"`
    Agents         []Agent         `yaml:"agents,omitempty"`
    Policies       []Policy        `yaml:"policies,omitempty"`
    AuthStrategies []AuthStrategy  `yaml:"auth_strategies,omitempty"`
    Consumers      []Consumer      `yaml:"consumers,omitempty"`   // credentials nested
    ConsumerGroups []ConsumerGroup `yaml:"consumer_groups,omitempty"`
    Vaults         []Vault         `yaml:"vaults,omitempty"`
    CACertificates []CACertificate `yaml:"ca_certificates,omitempty"`
    Certificates   []Certificate   `yaml:"certificates,omitempty"`
    SNIs           []SNI           `yaml:"snis,omitempty"`
}

func Parse(data []byte) (*Document, error)   // yaml.v3; YAML is a JSON superset
```

Its package doc states the entity types "mirror the schemas in `ai-gateway-admin-api.yaml`" — the
**same** OpenAPI spec `crd-from-oas/config.yaml` generates the `aiconfiguration` CRDs from. That
shared origin is what makes the translation layer cheap (see Proposal §3).

## Proposal

### 1. Architecture

```
aiconfiguration.konghq.com entity CRDs
  AIGatewayModel / ModelProvider / MCPServer / Agent / Policy /
  AuthStrategy / Consumer (+ ConsumerCredential) / ConsumerGroup
        │
        │  spec.aiGatewayRef.namespacedRef{group, kind, name}
        │      omitted    -> konnect.konghq.com/KonnectAIGateway   (today, unchanged)
        │      aigateway.konghq.com/OnPremAIGateway                (new)
        ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ OnPremAIGateway reconciler          controller/aigateway/onprem/         │
│                                                                          │
│  1. list child entities via field index (per kind)                       │
│  2. resolve Secret refs      <- reuse generated *_sdkops.go helpers      │
│  3. assemble *aigw.Document                                              │
│  4. convert.ConvertDocumentToDBLessYAML  ->  []byte                      │
│  5. discover ready admin API pods from the DP's admin EndpointSlice      │
│  6. per pod: skip-if-unchanged gate, else                                │
│       POST /config?check_hash=1&flatten_errors=1                         │
│  7. status: conditions + per-entity failure attribution                  │
└──────────────────────────────────────────────────────────────────────────┘
        │  mTLS, operator CA
        ▼
AIGatewayDataPlane  (spec.controlPlaneRef.type: onPremNamespacedRef)
  + NEW: admin listener :8444, admin Service, admin server cert
```

Direction of reference is DP → CP, matching `KonnectAIGateway` today. The CP does not select data
planes; it discovers the ones pointing at it.

### 2. `OnPremAIGateway` CRD — high-level sketch

`api/aigateway/v1alpha1/onpremaigateway_types.go`, group `aigateway.konghq.com`, namespaced,
`+kong:channels=kong-operator`. Modelled on `AIGatewayDataPlane`, which is the closest in-tree
template (new group, owns objects, deferred-status idiom).

```go
type OnPremAIGatewaySpec struct {
    // Conversion pins ai-deck-converter behaviour for this gateway. These are
    // not cosmetic: ModelSelectorSources selects between two mutually
    // exclusive on-the-wire ai-model-selector schemas, so it must track the
    // data plane version rather than be auto-detected.
    Conversion *ConversionOptions `json:"conversion,omitempty"`

    // Sync tunes push cadence and per-push timeout.
    Sync *SyncOptions `json:"sync,omitempty"`
}

type ConversionOptions struct {
    ModelSelectorSources *bool  `json:"modelSelectorSources,omitempty"` // convert.Options.ModelSelectorSources
    LabelTagPrefix       string `json:"labelTagPrefix,omitempty"`       // convert.Options.LabelTagPrefix
    Strict               *bool  `json:"strict,omitempty"`               // convert.Options.Strict
}

type OnPremAIGatewayStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Ready, ConfigurationValid, DataPlanesInSync

    // ConfigHash of the last successfully rendered configuration.
    ConfigHash string `json:"configHash,omitempty"`

    // DataPlanes reports per-AIGatewayDataPlane push state.
    DataPlanes []DataPlaneSyncStatus `json:"dataPlanes,omitempty"`
    // { name, readyPods, inSyncPods, lastPushedHash }
}
```

Deliberately absent, and why:

- **No data-plane selector.** Linkage is `AIGatewayDataPlane.spec.controlPlaneRef`, one direction,
  same as Konnect.
- **No `konnect` block, no `source`/`mirror`.** Those exist on `KonnectAIGatewaySpec` because it
  mirrors a remote Konnect entity. Nothing to mirror here.
- **No `endpoints` / `proxyUrls` in status.** `KonnectAIGatewayStatus.Endpoints` exists because
  Konnect hands the DP its endpoints. On-prem, the operator already knows the Services.
- **No AI Gateway runtime config.** That belongs on the DP CRD ([#4840](https://github.com/Kong/kong-operator/issues/4840)).

### 3. Translating the entity CRDs to ai-deck-converter format

Both sides derive from `ai-gateway-admin-api.yaml`. They differ only in naming convention and Go
representation:

| | CRD side | ai-deck-converter side |
| --- | --- | --- |
| Field naming | camelCase JSON (`displayName`) | snake_case YAML (`display_name`) |
| Serialization | `encoding/json` | `gopkg.in/yaml.v3` |
| Bool enums | `"Enabled"` / `"Disabled"` strings | `*bool` |

The camel → snake transform **already exists and is already generated per entity** — it is how
the Konnect SDK payloads are built:

```go
// api/aiconfiguration/v1alpha1/zz_generated_aigatewaypolicy_sdkops.go:138
func (s *AIGatewayPolicyAPISpec) marshalSDKOpsPayload() ([]byte, error) {
    // json.Marshal(apiSpec)
    // -> renameKeysToSDKExcept(payload, AIGatewayPolicySDKOpsFreeformKeyFields)  // camel -> snake,
    //      skipping free-form config maps whose keys are data, not field names
    // -> normalizeAIGatewayPolicySDKOpsBoolFields(payload)                       // "Enabled" -> true
    // -> json.Marshal
}

// api/aiconfiguration/v1alpha1/zz_generated_common_types.go:260
func renameKeysToSDKExcept(v any, fields []sdkOpsFreeformKeyField) any
```

Secret resolution is also already generated and reusable as-is:

```go
// zz_generated_aigatewaypolicy_sdkops.go:196 / :226
func (obj *AIGatewayPolicy) sdkOpsAPISpec(ctx context.Context, cl client.Client) (*AIGatewayPolicyAPISpec, error)
func (obj *AIGatewayPolicy) GetSensitiveDataSecretRefs() []SensitiveDataSecretRef
```

Because YAML is a superset of JSON, `aigw.Parse` consumes the snake_case JSON directly. The whole
translation layer is therefore an envelope assembler, not a field-by-field mapper:

```go
// controller/aigateway/onprem/document.go
func buildDocument(ctx context.Context, cl client.Client, children childSet) (*aigw.Document, error) {
    env := map[string][]json.RawMessage{}
    for _, m := range children.models {
        b, err := m.MarshalAIGatewayEntity(ctx, cl) // exported sibling of marshalSDKOpsPayload
        if err != nil {
            return nil, err
        }
        env["models"] = append(env["models"], b)
    }
    // ... one loop per kind, keys from the table below
    b, err := json.Marshal(env)
    if err != nil {
        return nil, err
    }
    return aigw.Parse(b) // yaml.v3 reads JSON
}
```

Kind → `Document` key:

| CRD kind | `aigw.Document` key |
| --- | --- |
| `AIGatewayModel` | `models` |
| `AIGatewayModelProvider` | `model_providers` |
| `AIGatewayMCPServer` | `mcp_servers` |
| `AIGatewayAgent` | `agents` |
| `AIGatewayPolicy` | `policies` |
| `AIGatewayAuthStrategy` | `auth_strategies` |
| `AIGatewayConsumer` | `consumers` |
| `AIGatewayConsumerGroup` | `consumer_groups` |
| `AIGatewayConsumerCredential` | nested under its parent consumer |
| `AIGatewayDataPlaneCertificate` | n/a — Konnect cert registration, not gateway config |

Work required:

1. **One `crd-from-oas` template change:** export the payload marshaller as e.g.
   `MarshalAIGatewayEntity(ctx, cl)` (= `sdkOpsAPISpec` + `marshalSDKOpsPayload`). Yields 10
   generated methods from a single template edit.
2. The envelope map above, ~10 lines.
3. `AIGatewayConsumerCredential` grouping: `aigw.Document` has no top-level credentials key, so
   group by `spec.aiGatewayConsumerRef` and nest. Note the ancestor hop this mirrors —
   `controller/konnect/reconciler_generic_handle_parentref.go:247`.
4. `aigw.Document` also has `vaults`, `certificates`, `ca_certificates`, `snis`. CRDs for these
   concepts do exist — `KongVault`, `KongCertificate`, `KongCACertificate`, `KongSNI`
   (`api/configuration/v1alpha1`) — but they belong to the wrong reference family: their
   `spec.controlPlaneRef` (`commonv1alpha1.ControlPlaneRef`) only accepts `konnectNamespacedRef`
   (→ `KonnectGatewayControlPlane`) or `kic`, with no value pointing at a `KonnectAIGateway` /
   `OnPremAIGateway` (`KongSNI` has no ref of its own; it points at a `KongCertificate` via
   `certificateRef` and inherits that certificate's control plane). None of the four can be listed
   via the `aiGatewayRef` parent-ref chain this design relies on. Their field shapes are also close
   but not identical to `aigw.Vault` / `Certificate` / `CACertificate` — e.g. `KongVaultSpec` has
   `Prefix` where `aigw.Vault` has `Name`, and `KongCACertificateAPISpec` has neither `CertDigest`
   nor `Labels`. Closing this properly means either extending `ControlPlaneRef` with an
   AI-Gateway-targeting variant, or adding dedicated `AIGatewayVault` / `AIGatewayCertificate` /
   `AIGatewayCACertificate` CRDs mirroring the rest of the `aiconfiguration` family. Out of scope
   for phase 1 either way; call out as a gap.

Verification: golden tests driven by `ai-deck-converter/convert/testdata/NN_*/input.yaml`, plus a
round-trip test decoding each `marshalSDKOpsPayload` output into the corresponding `aigw` type with
yaml.v3 `KnownFields(true)` so a dropped or renamed field fails loudly.

Two upstream asks for `ai-deck-converter`, neither a blocker:

- `aigw/doc.go` aliases 30 types but not `AuthStrategy` or `CACertificate`, so those `Document`
  fields cannot be named from outside the module. Going through `aigw.Parse` sidesteps it; add the
  aliases anyway.
- The typed `*kong.DBLessDocument` variant of the db-less converter is unexported
  (`convertDocumentToDBLess`). Bytes are all we need for the POST body, so no change required.

### 4. Pushing the configuration — what is and is not reusable

The half of the KIC pipeline usually meant by "the config synchronizer" —

```
Synchronizer -> KongClient.Update -> translator -> kongstate.KongState
             -> deckgen.ToDeckContent -> *file.Content -> UpdateStrategyInMemory
```

— **cannot carry AI Gateway configuration.** The reason is structural, not stylistic:

- `kongstate.KongState` (`ingress-controller/internal/dataplane/kongstate/kongstate.go`) has no
  AI-model field.
- decK's `file.Content` (go-database-reconciler v1.42.4, `pkg/file/types.go:1386`) has neither
  `ai_models` nor `consumer_group_consumers`. KIC shims the second in
  `sendconfig/inmemory_schema.go` (`DBLessConfig`); there is no shim for the first.
- Every AI Gateway model emits an `ai_models` entry (`ai-deck-converter/convert/dbless.go`,
  `out.AIModels`).

Routing converter output through `file.Content` would silently drop the primary entity. So don't.
And there is nothing to gain by trying: ai-deck-converter already produces the finished payload, so
the translator/KongState/deckgen layers have no work left to do.

Second constraint: **`ingress-controller/internal/...` is not importable from `controller/...`**
(Go internal visibility). Verified — nothing outside `ingress-controller/` imports it; the only two
matches are code-generator templates that emit import strings. This is also why
`controller/cpextensions/metricsscraper/admin_api_address_provider.go` already carries its own copy
of EndpointSlice admin discovery.

What remains reusable is still most of the value:

| Layer | Verdict |
| --- | --- |
| `POST /config?check_hash=1&flatten_errors=1` | **Reuse as-is.** `go-kong`'s `(*kong.Client).ReloadDeclarativeRawConfig(...)` (v0.78.0, `kong/client.go:519`). Already a direct dependency, importable anywhere in the repo. One call. |
| skip-if-unchanged + in-place restart detection | **Promote.** `sendconfig/config_change_detector.go` — sha256 compare, plus force-push when `GET /status` reports `configuration_hash == WellKnownInitialHash` (all zeros). |
| per-entity error attribution | **Promote.** `parseFlatEntityErrors` in `sendconfig/inmemory_error_handling.go` (currently unexported). |
| EndpointSlice → admin API discovery | **Promote.** `adminapi.Discoverer` / `AdminAPIsFromEndpointSlice` (`internal/adminapi/endpoints.go`). |
| ready/pending pod set management | **Promote, or defer.** `clients.AdminAPIClientsManager` (`internal/clients/manager.go`). Phase 1 can push to all ready endpoints without the promotion/demotion loop. |
| the 3s tick loop (`dataplane.Synchronizer`) | **Skip.** It exists because KIC has no watches on the objects it pushes. The `OnPremAIGateway` reconciler has real watches on the entity CRDs and the admin EndpointSlice. Keep only a modest `RequeueAfter` resync, so in-place pod restarts get re-detected via the `configuration_hash` sentinel. |
| translator / KongState / deckgen / `file.Content` / `UpdateStrategy*` | **Not reusable** (above). |

**Recommendation: promote, don't reimplement, and don't run a KIC instance.** Move the four
promotable rows into `ingress-controller/pkg/dataplane/push/`, leaving thin aliases behind in
`internal/`, and consume them from `controller/aigateway/onprem/`. The same move lets the duplicate
discovery in `controller/cpextensions/metricsscraper/` be retired rather than a third copy added —
risk #5 in the push-vs-pull RFC.

Rejected alternative: a KIC instance per `OnPremAIGateway` via `ingress-controller/pkg/manager` +
`multiinstance`, the way `controller/controlplane` does it. It would deliver the tick loop,
discovery, readiness and recovery ladder for free, but `KongConfigBuilder` yields
`*kongstate.KongState` and `KongClient.sendToClient` hardcodes `deckgen.ToDeckContent`, so both
would have to be widened to carry raw bytes, plus a new `managercfg` mode flag to select it. That
is more surgery inside KIC than the reuse is worth.

### 5. `AIGatewayDataPlane` changes

Today the AI Gateway pod exposes the ingress listener (8443) and the status listener only —
`controller/aigateway/dataplane/consts.go` sets no `KONG_ADMIN_LISTEN`, and `owned_service.go`
creates only the ingress Service. Push mode needs:

- `KONG_ADMIN_LISTEN=0.0.0.0:8444 ssl`, `KONG_ADMIN_SSL_CERT` / `_KEY`, and `verify_client=on`
  against the operator CA. Confirmed with the AI Gateway data plane team: the admin listener is
  configured the same way as classic Kong Gateway, and `POST /config` / `GET /status` behave the
  same way as Kong EE — `check_hash`, `flatten_errors`, and the `configuration_hash` sentinel all
  apply unchanged.
- An admin Service, with a port name the discoverer can filter on.
- An admin **server** certificate. `owned_secret.go` currently issues a *client* cert for Konnect
  mTLS; `controller/pkg/secrets.EnsureCertificate` handles both. The wildcard-per-admin-Service
  plus fixed-SNI-hostname workaround (`pod.<svc>.<ns>.svc`) is inherited from KIC unless the
  reverted PR #1950 approach is redone — worth doing here, since this is greenfield with no
  upgrade path to break.
- `spec.controlPlaneRef.type: onPremNamespacedRef` + an `OnPremNamespacedRef` field
  (`api/aigateway/v1alpha1/controlplaneref_types.go`, currently a single-value enum).
- `buildAIGatewayEnvVars` must stop emitting `KONG_ROLE=data_plane`, `KONG_CLUSTER_*` and
  `KONG_KONNECT_MODE=on` in on-prem mode. Certificate issuance (step 2 of the existing reconcile)
  is already Konnect-agnostic and carries over unchanged; the Konnect cert *registration* step
  (`ensureKonnectCertificate`) is skipped entirely.

### 6. `aiGatewayRef`

Chosen shape: keep `type: namespacedRef` and add optional `group` + `kind`, defaulting to
`konnect.konghq.com/KonnectAIGateway`, so existing objects are untouched.

```yaml
# unchanged, still Konnect
aiGatewayRef:
  type: namespacedRef
  namespacedRef:
    name: my-konnect-aigw
---
# new
aiGatewayRef:
  type: namespacedRef
  namespacedRef:
    group: aigateway.konghq.com
    kind: OnPremAIGateway
    name: my-onprem-aigw
```

One refinement, with the reason: introduce this as a **new ref type used only by `aiGatewayRef`**
rather than widening the shared `commonv1alpha1.NamespacedRef`. That shared Go type backs
`serviceRef`, `certificateRef`, `konnectExtension`, `dataPlaneMetricsExtension` and others across
~57 API files, where `group`/`kind` would be meaningless schema bloat — and the chart's CRD payload
has already brushed the 1 MB Secret limit once. Because the type is shared, the default also needs
care: the "no group/kind means `KonnectAIGateway`" default must live in the `aiGatewayRef`-specific
wrapper type, not on `NamespacedRef` itself, or every other CRD embedding `NamespacedRef` inherits
a default that means nothing to it.

Consequences to handle:

1. **`GetParentGVK()` must become ref-derived.** It is generated per kind and returns a fixed GVK
   today (`zz_generated_aigatewaymodel_funcs.go`).
2. **The generic Konnect reconciler must skip on-prem-targeted entities.** All 10 kinds are
   registered on `KonnectEntityReconciler` in
   `modules/manager/zz_generated_konnect_controller_setup.go`. An entity whose ref names an
   `OnPremAIGateway` has no Konnect ID, no `Programmed`-on-Konnect and no
   `KonnectAPIAuthConfiguration` to resolve — reconciling it as a Konnect entity would attempt to
   create it in Konnect. A predicate belongs at
   `controller/konnect/reconciler_generic_handle_generated_refs.go:119`, where
   `UnsupportedGeneratedReferenceTypeError` is raised today.
3. **New index + watch per kind**, mirroring `internal/utils/index/zz_generated_aigateway*.go`
   (whose extractors read `Spec.AIGatewayRef.NamespacedRef` only, so konnectID refs are already
   unindexed).
4. **Status shape.** `status.gatewayID`, the inlined `KonnectEntityStatus` and the
   `KonnectAIGatewayRefValid` condition are all Konnect-shaped. On-prem entities need their own
   condition plus a "pushed to N/M data planes" signal, and must leave the Konnect fields empty.
   Note also that the per-kind immutability rule — *"spec.aiGatewayRef is immutable when an entity
   is already Programmed"* — keys on `Programmed`, which on-prem never sets, so the ref would stay
   mutable. Decide whether that is acceptable or needs a parallel rule.

### 7. Suggested phasing

| # | Scope | Independently testable? |
| --- | --- | --- |
| 1 | `OnPremAIGateway` CRD + reconciler skeleton + status/conditions. No push. | Yes — envtest + CRD validation tests |
| 2 | Document assembly + ai-deck-converter integration + golden tests. Render the config into status or a ConfigMap for inspection. No push. | Yes — unit/golden, no DP changes needed |
| 3 | `AIGatewayDataPlane`: admin listener, admin Service, admin server cert, `onPremNamespacedRef`, env-var branch. | Yes — envtest + integration |
| 4 | Promote push machinery to `ingress-controller/pkg/`; wire the push; per-entity error attribution; conditions. Retire the duplicate discovery in `cpextensions/metricsscraper`. | Yes — integration/e2e |
| 5 | `aiGatewayRef` group/kind, Konnect-reconciler skip predicate, indexes and watches. | Yes — independent of 2–4, but blocks real end-to-end use |

Phase 2 is the highest-information-per-unit-of-work step and depends on nothing else, so it is the
right place to start if the CRD shape is still in flux.

### 8. Open questions — AI Gateway data plane team

Carried over from the push-vs-pull RFC:

1. ~~Does aigw 2.0's Admin API support `POST /config` with `check_hash` / `flatten_errors`? Does
   `GET /status` expose `configuration_hash` with the same all-zeros "unconfigured" sentinel?~~
   **Answered: this works the same way as in Kong EE.**
2. ~~Is the admin listener configured the same way as classic Kong
   (`KONG_ADMIN_LISTEN`, `verify_client=on`)?~~ **Answered: this works the same way as in Kong EE.**

New, from this design:

1. Does aigw accept `ai_models` in the dbless `POST /config` payload, and does it accept the exact
   output of `ai-deck-converter -direction to-dbless` unmodified? A golden-payload contract test in
   one of the two repos would settle this permanently.
2. Which `ModelSelectorSources` schema does each aigw version accept? The two are mutually
   exclusive on the wire and cannot be auto-detected, so the operator must pin it per gateway
   (hence `spec.conversion.modelSelectorSources`) — we need a version-to-schema mapping.
3. Does `ai-deck-converter`'s `ConversionError.Diagnostics` (`{Field, Messages}`) carry enough
   source identity to attribute a failure back to a specific model/policy/agent? If yes, translation
   failures can surface as conditions on the owning entity CR, not just on the gateway.

### 9. Open questions — operator team

1. Should on-prem entity CRs get their own `Programmed`-equivalent condition, or should
   `Programmed` be reused with on-prem semantics ("present in the last successfully pushed
   config")? This decides whether the existing `aiGatewayRef`-immutability CEL keeps working.
2. Fan-out sizing: every push reaches every ready pod. Worth sizing against expected AI Gateway
   fleet sizes rather than assuming classic-Gateway scale carries over.
3. Certificate rotation: the operator CA becomes the sole trust root for every AI Gateway pod, for
   both directions. Rotation must not force a synchronized fleet-wide restart.
4. HA: discovery runs without leader election in KIC so every replica keeps its own view, while the
   `Synchronizer` is leader-elected. With the loop replaced by a reconciler, leader election comes
   from the manager — confirm that is the intended behaviour for pushes.

## Verification of claims in this brief

- `rg -n 'ai_models' $(go env GOMODCACHE)/github.com/kong/go-database-reconciler@v1.42.4/pkg/file/types.go`
  returns nothing — confirms decK's `file.Content` has no AI-model field.
- `rg -l 'kong-operator/v2/ingress-controller/internal' --glob '!ingress-controller/**' --glob '*.go' .`
  returns only two `hack/generators/` template files — confirms `internal/` is not importable from
  `controller/`.
