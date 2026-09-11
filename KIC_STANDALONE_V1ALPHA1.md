# KIC Standalone Support for v1alpha1 Kong Gateway Entity CRDs

## Overview

This document describes the changes made to support the v1alpha1 Kong Gateway entity CRDs
(`KongService`, `KongRoute`, `KongUpstream`, `KongTarget`, `KongCertificate`, `KongCACertificate`,
`KongSNI`, `KongPluginBinding`) in **KIC standalone db-less mode**, without requiring a Konnect
control plane.

### Background

These CRDs were originally designed for the Konnect pipeline (Pipeline 2): a reconciler reads
the CR and calls the Konnect REST API via `sdk-konnect-go`. A CRD-level XValidation rule
(`self.controlPlaneRef.type != 'kic'`) explicitly blocked their use in KIC standalone mode.

The goal of this change is to add a **KIC standalone path** (Pipeline 1) where these CRDs are
translated directly to `kong.*` types (from `github.com/kong/go-kong`) and pushed to the Kong
Admin API via `POST /config` in db-less mode, with Kubernetes as the sole source of truth.
The Konnect path remains entirely unchanged.

---

## Architecture

```
K8s CRDs (v1alpha1)
       │
       ├── [Pipeline 1 – KIC standalone, this change]
       │       KIC watches CRs → Store → Translator → KongState (kong.*) → POST /config (db-less)
       │
       └── [Pipeline 2 – Konnect, unchanged]
               reconciler_generic reads CRs → sdk-konnect-go → Konnect REST API
```

The two pipelines are independent. Enabling `KongServiceV1Alpha1` feature gate activates
Pipeline 1 without affecting Pipeline 2.

---

## Feature Gate

A new opt-in feature gate controls the KIC standalone path:

| Feature gate | Default | Description |
|---|---|---|
| `KongServiceV1Alpha1` | `false` | Enables KIC standalone translation of v1alpha1 Kong entity CRDs |

To enable it, set the feature gate in the manager configuration:

```yaml
feature-gates: KongServiceV1Alpha1=true
```

---

## Supported CRD Types

| CRD (v1alpha1) | Dependency | Maps to KongState |
|---|---|---|
| `KongService` | none | `KongState.Services` |
| `KongRoute` | `serviceRef → KongService` | `Service.Routes` |
| `KongUpstream` | none | `KongState.Upstreams` |
| `KongTarget` | `upstreamRef → KongUpstream` | `Upstream.Targets` |
| `KongCertificate` | optional `secretRef → k8s Secret` | `KongState.Certificates` |
| `KongCACertificate` | optional `secretRef → k8s Secret` | `KongState.CACertificates` |
| `KongSNI` | `certificateRef → KongCertificate` | `Certificate.SNIs` |
| `KongPluginBinding` | `pluginRef → KongPlugin/KongClusterPlugin` + targets | `KongState.Plugins` |

> `KongKey` and `KongKeySet` are out of scope (no native field in the standard go-kong KongState).

### Certificate sourcing

`KongCertificate` and `KongCACertificate` support two source types:

- **`inline`**: certificate data is provided directly in the CR spec (`cert`, `key`, `certAlt`, `keyAlt`).
- **`secretRef`**: certificate data is read from a Kubernetes Secret:
  - `KongCertificate`: secret must contain `tls.crt` and `tls.key`
  - `KongCACertificate`: secret must contain `ca.crt`

### Type conversion bridge

The v1alpha1 CRD specs use types from `github.com/Kong/sdk-konnect-go/models/components`
while KongState uses types from `github.com/kong/go-kong`. Both represent the same Kong API
entities and share identical JSON field names, so complex nested types (e.g., `Healthchecks`)
are converted via a JSON marshal/unmarshal round-trip:

```go
func convertViaJSON[F, T any](from F) (T, error) {
    b, _ := json.Marshal(from)
    var to T
    return to, json.Unmarshal(b, &to)
}
```

---

## Files Changed

### 1. CRD type validation — removed blocking XValidation rule

The rule `self.controlPlaneRef.type != 'kic'` was removed from the spec of:

| File |
|---|
| `api/configuration/v1alpha1/kongservice_types.go` |
| `api/configuration/v1alpha1/kongroute_types.go` |
| `api/configuration/v1alpha1/kongupstream_types.go` |
| `api/configuration/v1alpha1/kongcertificate_types.go` |
| `api/configuration/v1alpha1/kongcacertificate_types.go` |
| `api/configuration/v1alpha1/kongdataplaneclientcertificate_types.go` |
| `api/configuration/v1alpha1/kongkey_types.go` |
| `api/configuration/v1alpha1/kongkeyset_types.go` |

### 2. Store generator spec

**`hack/generators/cache-stores/spec.go`** — Added 8 new entries to `supportedTypes`:

```go
{Type: "KongService",       Package: "kongv1alpha1", StoreField: "KongServiceV1Alpha1"},
{Type: "KongRoute",         Package: "kongv1alpha1", StoreField: "KongRouteV1Alpha1"},
{Type: "KongUpstream",      Package: "kongv1alpha1", StoreField: "KongUpstreamV1Alpha1"},
{Type: "KongTarget",        Package: "kongv1alpha1", StoreField: "KongTargetV1Alpha1"},
{Type: "KongCertificate",   Package: "kongv1alpha1", StoreField: "KongCertificateV1Alpha1"},
{Type: "KongCACertificate", Package: "kongv1alpha1", StoreField: "KongCACertificateV1Alpha1"},
{Type: "KongSNI",           Package: "kongv1alpha1", StoreField: "KongSNIV1Alpha1"},
{Type: "KongPluginBinding", Package: "kongv1alpha1", StoreField: "KongPluginBindingV1Alpha1"},
```

### 3. Generated cache store

**`ingress-controller/internal/store/zz_generated.cache_stores.go`** — Updated manually
(generator not re-run) to add 8 new cache store fields, their initialization, `Get`/`Add`/`Delete`
switch cases, and entries in `ListAllStores()` / `SupportedTypes()`.

### 4. Store interface and implementation

**`ingress-controller/internal/store/store.go`** — Added 8 new methods to the `Storer` interface
and their implementations, following the `ListKongVaults()` pattern with `isValidIngressClass`
filtering:

```go
ListKongServicesV1Alpha1() []*configurationv1alpha1.KongService
ListKongRoutesV1Alpha1() []*configurationv1alpha1.KongRoute
ListKongUpstreamsV1Alpha1() []*configurationv1alpha1.KongUpstream
ListKongTargetsV1Alpha1() []*configurationv1alpha1.KongTarget
ListKongCertificatesV1Alpha1() []*configurationv1alpha1.KongCertificate
ListKongCACertificatesV1Alpha1() []*configurationv1alpha1.KongCACertificate
ListKongSNIsV1Alpha1() []*configurationv1alpha1.KongSNI
ListKongPluginBindingsV1Alpha1() []*configurationv1alpha1.KongPluginBinding
```

### 5. Fake store

**`ingress-controller/internal/store/fake_store.go`** — Added 8 new fields to `FakeObjects`
and their initialization in `NewFakeStore()`.

### 6. Fallback dependency graph

**`ingress-controller/internal/dataplane/fallback/graph_dependencies.go`** — Added explicit
switch cases for all 8 new types.

Types with no dependencies return `nil`:
- `KongService`, `KongUpstream`, `KongCertificate`, `KongCACertificate`

Types with dependencies have dedicated resolve functions:
- `KongRoute` → resolves `serviceRef.namespacedRef` → `KongService`
- `KongTarget` → resolves `upstreamRef` → `KongUpstream`
- `KongSNI` → resolves `certificateRef` → `KongCertificate`
- `KongPluginBinding` → resolves `pluginReference` → `KongPlugin`/`KongClusterPlugin` + optional targets

### 7. Feature gate constant

**`ingress-controller/pkg/manager/config/feature_gates_keys.go`** — Added:

```go
const KongServiceV1Alpha1Feature = "KongServiceV1Alpha1"
// default: false (opt-in)
```

### 8. Translator feature flags

**`ingress-controller/internal/dataplane/translator/translator.go`** — Added `KongServiceV1Alpha1 bool`
to `FeatureFlags` and wired it to the feature gate in `NewFeatureFlags()`.

The `BuildKongConfig()` method now calls the three new Fill methods when the flag is set:

```go
if t.featureFlags.KongServiceV1Alpha1 {
    result.FillFromKongServicesV1Alpha1(t.logger, t.storer, t.failuresCollector)
    result.FillFromKongCertificatesV1Alpha1(t.logger, t.storer, t.failuresCollector)
    result.FillFromKongPluginBindingsV1Alpha1(t.logger, t.storer, t.failuresCollector)
}
```

### 9. KongState translation layer (new file)

**`ingress-controller/internal/dataplane/kongstate/kongservices_v1alpha1.go`** — New file
implementing the full translation from v1alpha1 CRs to `kong.*` KongState types:

| Method | Description |
|---|---|
| `FillFromKongServicesV1Alpha1` | Translates `KongUpstream`+`KongTarget` → `Upstream`, `KongService`+`KongRoute` → `Service` |
| `FillFromKongCertificatesV1Alpha1` | Translates `KongCertificate` → `Certificate`, `KongCACertificate` → `CACertificate`, `KongSNI` → `Certificate.SNIs` |
| `FillFromKongPluginBindingsV1Alpha1` | Translates `KongPluginBinding` → `Plugin` (with target resolution) |

Translation order within `FillFromKongServicesV1Alpha1`:
1. `KongUpstream` → build upstream map
2. `KongTarget` → attach to upstreams by `upstreamRef`
3. `KongService` → build service map
4. `KongRoute` → attach to services by `serviceRef.namespacedRef`; serviceless routes get a placeholder `0.0.0.0:1` service

### 10. Controller definitions

**`ingress-controller/internal/manager/controllerdef.go`** — Added 8 new `ControllerDef` entries,
all guarded by `featureGates.Enabled(managercfg.KongServiceV1Alpha1Feature)`.

### 11. Generated reconcilers

**`ingress-controller/internal/controllers/configuration/zz_generated.controllers.go`** — Added
8 new reconciler structs following the `KongV1Alpha1KongCustomEntityReconciler` pattern:

- `KongV1Alpha1KongServiceReconciler`
- `KongV1Alpha1KongRouteReconciler`
- `KongV1Alpha1KongUpstreamReconciler`
- `KongV1Alpha1KongTargetReconciler`
- `KongV1Alpha1KongCertificateReconciler`
- `KongV1Alpha1KongCACertificateReconciler`
- `KongV1Alpha1KongSNIReconciler`
- `KongV1Alpha1KongPluginBindingReconciler`

Each reconciler implements `SetupWithManager`, `Reconcile`, `SetLogger`, `listClassless`
and the standard KIC reconciliation loop (get → delete-on-not-found → deletion-timestamp check →
ingress-class filter → UpdateObject → status update).

---

## Usage Example

```yaml
# KongUpstream
apiVersion: configuration.konghq.com/v1alpha1
kind: KongUpstream
metadata:
  name: my-upstream
  namespace: default
spec:
  name: my-upstream
  algorithm: round-robin
---
# KongService
apiVersion: configuration.konghq.com/v1alpha1
kind: KongService
metadata:
  name: my-service
  namespace: default
spec:
  host: my-upstream
  port: 80
  protocol: http
---
# KongRoute
apiVersion: configuration.konghq.com/v1alpha1
kind: KongRoute
metadata:
  name: my-route
  namespace: default
spec:
  serviceRef:
    type: namespacedRef
    namespacedRef:
      name: my-service
  protocols:
    - http
  paths:
    - /api
```

---

## What Was Not Changed

- The Konnect reconciliation pipeline (`controller/konnect/`) is untouched.
- `KongKey` and `KongKeySet` are not supported in this implementation.
- No new CRD schemas were introduced; only existing v1alpha1 CRDs are used.
- `make manifests` / `make generate` were not re-run; CRD YAML files are unchanged
  (the removed XValidation rule requires a `make manifests` run before deploying updated CRDs).

---

## Post-merge Steps

Before deploying to a cluster:

```bash
make manifests          # Regenerate CRD YAML after XValidation rule removal
make generate           # Regenerate deepcopy, store, docs
make test.unit          # Verify unit tests pass
make test.envtest       # Verify controller tests pass
```
