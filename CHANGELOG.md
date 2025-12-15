# Changelog

## Table of Contents

- [v2.1.0-alpha.0](#v210-alpha0)
- [v2.0.5](#v205)
- [v2.0.4](#v204)
- [v2.0.3](#v203)
- [v2.0.2](#v202)
- [v2.0.1](#v201)
- [v2.0.0](#v200)
- [v1.6.2](#v162)
- [v1.6.1](#v161)
- [v1.6.0](#v160)
- [v1.5.1](#v151)
- [v1.5.0](#v150)
- [v1.4.2](#v142)
- [v1.4.1](#v141)
- [v1.4.0](#v140)
- [v1.3.0](#v130)
- [v1.2.3](#v123)
- [v1.2.2](#v122)
- [v1.2.1](#v121)
- [v1.2.0](#v120)
- [v1.1.0](#v101)
- [v1.0.3](#v103)
- [v1.0.2](#v102)
- [v1.0.1](#v101)
- [v1.0.0](#v100)
- [v0.7.0](#v070)
- [v0.6.0](#v060)
- [v0.5.0](#v050)
- [v0.4.0](#v040)
- [v0.3.0](#v030)
- [v0.2.0](#v020)
- [v0.1.1](#v011)
- [v0.1.0](#v010)

## Unreleased

### Added

- `DataPlane`: Enable incremental config sync by default when using Konnect as control plane.
  This improves performance of config syncs for large configurations.
  [#2759](https://github.com/Kong/kong-operator/pull/2759)
- `KongCertificate`: Add support for sourcing certificates from Kubernetes Secrets.
  This allows users to define KongCertificates that reference existing Kubernetes
  Secrets containing TLS certificate and key data, instead of embedding them inline.
  [#2802](https://github.com/Kong/kong-operator/pull/2802)  
- `KongCACertificate`: Add support for sourcing CA certificates from Kubernetes Secrets.
  This allows users to define KongCACertificates that references exsiting Kubernetes
  Secrets containing TLS CA certificate instead of embedding them inline
  [#2482](https://github.com/Kong/kong-operator/pull/2842)
- `KongReferenceGrant` CRD has been added to allow cross-namespace references
  among Konnect entities API. This new resource is to be intended as the Kong
  version of the original Gateway API `ReferenceGrant` CRD.
  [#2855](https://github.com/Kong/kong-operator/pull/2855)
- Hybdrid Gateway: the creation and deletion of the Kong resources derived from `HTTPRoute`s is now
  performed in multiple steps that account for dependencies among the generated resources.
  [#2857](https://github.com/Kong/kong-operator/pull/2857)

### Changed

- `DataPlane`'s `spec.network.services.ingress.ports` now allows up to 64 ports
  to be specified. This aligns `DataPlane` with Gateway APIs' `Gateway`.
  [#2722](https://github.com/Kong/kong-operator/pull/2722)

### Fixed

- Fixed an issue where users could set the secret of configmap label selectors
  to empty when the other one was left non-empty.
  [#2810](https://github.com/Kong/kong-operator/pull/2810)

## [v2.1.0-alpha.0]

### Added

- Hybrid Gateway support: Gateway API objects bound to `Gateway`s programmed in Konnect
  are converted into Konnect entities and used to configure the hybrid `DataPlane`.
  [#2134](https://github.com/Kong/kong-operator/pull/2134)
  [#2143](https://github.com/Kong/kong-operator/pull/2143)
  [#2177](https://github.com/Kong/kong-operator/pull/2177)
  [#2260](https://github.com/Kong/kong-operator/pull/2260)
- Add comprehensive HTTPRoute reconciliation that translates Gateway API
  HTTPRoutes into Kong-specific resources for hybrid gateway deployments.
  [#2308](https://github.com/Kong/kong-operator/pull/2308)
- Hybrid Gateway: add support to HTTPRoute hostnames translation
  [#2346](https://github.com/Kong/kong-operator/pull/2346)
  - Enforce state and cleanup for Kong entities
  - Introduced managedfields package for structured merge diff, including compare, extract, prune, and schema utilities with comprehensive tests.
  - Refactored builder and converter logic for KongRoute, KongService, KongTarget, KongUpstream, and HTTPRoute.
  - Enhanced metadata labeling and reconciliation logic for HTTPRoute; added resource ownership tracking via watches.
  - Added generated schema in zz_generated_schema.go for resource types.
  - Improved and extended unit tests for hybridgateway components.
  [2355](https://github.com/Kong/kong-operator/pull/2355)
- Hybrid Gateway: add Konnect specific fields to `GatewayConfiguration` CRD.
  [#2390](https://github.com/Kong/kong-operator/pull/2390)
  [#2405](https://github.com/Kong/kong-operator/pull/2405)
- Hybrid Gateway: implement granular accepted and programmed conditions for HTTPRoute status
  This commit introduces comprehensive support for "Accepted" and "Programmed" status conditions
  on HTTPRoute resources in the hybridgateway controller. The new logic evaluates each ParentReference
  for controller ownership, Gateway/GatewayClass support, listener matching, and resource programming
  status. For every relevant Kong resource (KongRoute, KongService, KongTarget, KongUpstream, KongPlugin, KongPluginBinding),
  the controller sets detailed programmed conditions, providing clear feedback on which resources are operational
  and which are not.
  The update also refactors builder and metadata logic to ensure labels and annotations are correctly set for
  all managed resources, and improves test coverage for label, annotation, and hostname intersection handling.
  Legacy status controller code is removed, and the reconciliation flow is streamlined to use the new status
  enforcement and translation logic.
  This enables more robust troubleshooting and visibility for users, ensuring HTTPRoute status accurately reflects
  the readiness and configuration of all associated Kong resources.
  [#2400](https://github.com/Kong/kong-operator/pull/2400)
- ManagedFields: improve pruning of empty fields in unstructured objects
  - Enhance pruneEmptyFields to recursively remove empty maps from slices and maps, including those that become empty after nested pruning.
  - Update logic to remove empty slices and zero-value fields more robustly.
  - Expand and refine unit tests in prune_test.go to cover all edge cases, including:
    - Nested empty maps and slices
    - Removal of empty maps from slices
    - Handling of mixed-type slices
    - Deeply nested pruning scenarios
    - Preservation of non-map elements in slices
  [#2413](https://github.com/Kong/kong-operator/pull/2413)
- Entity Adoption support: support adopting an existing entity from Konnect to
  a Kubernetes custom resource for managing the existing entity by KO.
  - Add adoption options to the CRDs supporting adopting entities from Konnect.
    [#2336](https://github.com/Kong/kong-operator/pull/2336)
  - Add `adopt.mode` field to the CRDs that support adopting existing entities.
    Supported modes:
    - `match`: read-only adoption. The operator adopts the referenced remote entity
      only when this CR's spec matches the remote configuration
      (no writes to the remote system).
      If they differ, adoption fails and the operator does not take ownership until
      the spec is aligned.
    - `override`: The operator overrides the remote entity with the spec in the CR.
    [#2421](https://github.com/Kong/kong-operator/pull/2421)
    [#2424](https://github.com/Kong/kong-operator/pull/2424)
  - Implement the general handling process of adopting an existing entity and
    adoption procedure for `KongService`s in `match` and `override` mode.
    [#2424](https://github.com/Kong/kong-operator/pull/2424)
  - Implement the Match mode for adoption for Konnect cloud gateway entities
    [#2429](https://github.com/Kong/kong-operator/pull/2429)
  - Implement adoption support for `KongCertificate`, `KongCACertificate` and `KongSNI`
    [#2484](https://github.com/Kong/kong-operator/pull/2484)
  - Implement adoption support for `KongVault`.
    [#2490](https://github.com/Kong/kong-operator/pull/2490)
  - Implement adoption for `KongKey` and `KongKeySet` resources
    [#2487](https://github.com/Kong/kong-operator/pull/2487)
  - Implement adoption support for `KongConsumer` and `KongConsumerGroup`
    [#2493](https://github.com/Kong/kong-operator/pull/2493)
  - Implement adoption for `KongPluginBinding`.
    [#2492](https://github.com/Kong/kong-operator/pull/2492)
  - Implement adoption support for `KongCredentialAPIKey`, `KongCredentialBasicAuth`, `KongCredentialACL`, `KongCredentialJWT`, and `KongCredentialHMAC`
    [#2494](https://github.com/Kong/kong-operator/pull/2494)
  - Implement adoption support for `KongDataPlaneClientCertificate`.
    [#2678](https://github.com/Kong/kong-operator/pull/2678)
- HybridGateway:
  - Added controller-runtime watches for Gateway and GatewayClass resources to the hybridgateway controller.
  - HTTPRoutes are now reconciled when related Gateway or GatewayClass resources change.
  - Improved event mapping and indexing logic for efficient reconciliation.
  - Added unit tests for new watch and index logic.
  [#2419](https://github.com/Kong/kong-operator/pull/2419)
- Provision hybrid Gateway: implement support for provisioning hybrid Gateways with
  gateway api `Gateway` and `GatewayConfiguration` resources.
  [#2457](https://github.com/Kong/kong-operator/pull/2457)
- Add support to HTTPRoute RequestRedirect filter
  [#2470](https://github.com/Kong/kong-operator/pull/2470)
- Add CLI flag `--enable-fqdn-mode` to enable Fully Qualified Domain Name (FQDN)
  mode for service discovery. When enabled, Kong targets are configured to use
  service FQDNs (e.g., `service.namespace.svc.cluster.local`) instead of
  individual pod endpoint IPs.
  [#2607](https://github.com/Kong/kong-operator/pull/2607)
- Gateway: support per-Gateway infrastructure configuration
  [GEP-1867](https://gateway-api.sigs.k8s.io/geps/gep-1867/) via
  `GatewayConfiguration` CRD.
  [#2653](https://github.com/Kong/kong-operator/pull/2653)
- HybridGateway: reworked generated resources lifecycle management. HTTPRoute ownership on the resources
  is now tracked through the `gateway-operator.konghq.com/hybrid-routes` annotation. The same generated
  resource can now be shared among different HTTPRoutes.
  [#2656](https://github.com/Kong/kong-operator/pull/2656)
- HybridGateway: implemented `ExtensionRef` filters to allow reference of self-managed plugins from
  `HTTPRoute`s' filters.
  [#2715](https://github.com/Kong/kong-operator/pull/2715)
- `KonnectAPIAuthConfiguration` resources now have automatic finalizer management
  to prevent deletion when they are actively referenced by other Konnect resources
  (`KonnectGatewayControlPlane`, `KonnectCloudGatewayNetwork`, `KonnectExtension`).
  The finalizer `konnect.konghq.com/konnectapiauth-in-use` is automatically added
  when references exist and removed when all referencing resources are deleted.
  [#2726](https://github.com/Kong/kong-operator/pull/2726)
- Add the following configuration flags for setting the maximum number of concurrent
  reconciliation requests that can be processed by each controller group:
  - `--max-concurrent-reconciles-dataplane-controller` for DataPlane controllers.
  - `--max-concurrent-reconciles-controlplane-controller` for ControlPlane controllers.
  - `--max-concurrent-reconciles-gateway-controller` for Gateway controllers.

  NOTE: Konnect entities controllers still respect the
  `--konnect-controller-max-concurrent-reconciles` flag.
  [#2652](https://github.com/Kong/kong-operator/pull/2652)

### Changed

- kong/kong-gateway v3.12 is the default proxy image. [#2391](https://github.com/Kong/kong-operator/pull/2391)
- For Hybrid `Gateway`s the operator does not run the `ControlPlane` anymore, as
  the `DataPlane` is configured to use `Koko` as Konnect control plane.
  [#2253](https://github.com/Kong/kong-operator/pull/2253)
- HybridGateway auto-generated resource names has been revised.
  [#2566](https://github.com/Kong/kong-operator/pull/2566)
- Update Gateway API to 1.4.0 and k8s libraries to 1.34.
  [#2451](https://github.com/Kong/kong-operator/pull/2451)

### Fixes

- Hybrid Gateway: generate a single KongRoute for each HTTPRoute Rule
  [#2417](https://github.com/Kong/kong-operator/pull/2417)
- Fix issue with deletion of `KonnectExtension` when the referenced
  `KonnectGatewayControlPlane` is deleted (it used to hang indefinitely).
  [#2423](https://github.com/Kong/kong-operator/pull/2423)
- Hybrid Gateway: add watchers for KongPlugin and KongPluginBinding
  [#2427](https://github.com/Kong/kong-operator/pull/2427)
- Hybrid Gateway: attach KongService generation to BackendRefs and fix filter/plugin conversion.
  [#2456](https://github.com/Kong/kong-operator/pull/2456)
- Translate `healtchchecks.threshold` in `KongUpstreamPolicy` to the
  `healthchecks.threshold` field in Kong upstreams.
  [#2662](https://github.com/Kong/kong-operator/pull/2662)
- Reject CA Secrets with multiple PEM certs.
  [#2671](https://github.com/Kong/kong-operator/pull/2671)
- Fix the default values of `combinedServicesFromDifferentHTTPRoutes` and
  `drainSupport` in `ControlPlaneTranslationOptions` not being set correctly.
  [#2589](https://github.com/Kong/kong-operator/pull/2589)
- Fix random, unexpected and invalid validation error during validation of `HTTPRoute`s
  for `Gateway`s configured in different namespaces with `GatewayConfiguration` that
  has field `spec.controlPlaneOptions.watchNamespaces.type` set to `own`.
  [#2717](https://github.com/Kong/kong-operator/pull/2717)
- Gateway controllers now watch changes on Secrets referenced by
  `spec.listeners.tls.certificateRef`, ensuring Gateway status conditions
  are updated when referenced certificates change.
  [#2661](https://github.com/Kong/kong-operator/pull/2661)

## [v2.0.5]

> Release date: 2025-10-17

### Fixes

- Fix `DataPlane`'s volumes and volume mounts patching when specified by user
  [#2425](https://github.com/Kong/kong-operator/pull/2425)
- Do not cleanup `null`s in the configuration of plugins with Kong running in
  DBLess mode in the translator of ingress-controller. This enables user to use
  explicit `null`s in plugins.
  [#2459](https://github.com/Kong/kong-operator/pull/2459)

## [v2.0.4]

> Release date: 2025-10-03

### Fixes

- Fix problem with starting operator when Konnect is enabled and conversion webhook disabled.
  [#2392](https://github.com/Kong/kong-operator/issues/2392)

## [v2.0.3]

> Release date: 2025-09-30

### Fixes

- Do not validate `Secret`s and `ConfigMap`s that are used internally by the operator.
  This prevents issues when those resources are created during bootstrapping of the
  operator, before the validating webhook is ready.
  [#2356](https://github.com/Kong/kong-operator/pull/2356)
- Add the `status.clusterType` in `KonnectGatewayControlPlane` and set it when
  KO attached the `KonnectGatewayControlPlane` with the control plane in
  Konnect. The `KonnectExtension` now get the cluster type to fill its
  `status.konnect.clusterType` from the `statusType` of `KonnectGatewayControlPlane`
  to fix the incorrect cluster type filled in the status when the control plane
  is mirrored from an existing control plane in Konnect.
  [#2343](https://github.com/Kong/kong-operator/pull/2343)

## [v2.0.2]

> Release date: 2025-09-22

### Fixes

- Cleanup old objects when new `ControlPlane` is ready.
  Remove old finalizers from `ControlPlane` when cleanup is done.
  [#2317](https://github.com/Kong/kong-operator/pull/2317)
- Mark `Gateway`'s listeners as Programmed when `DataPlane` and its `Services` are ready.
  This prevents downtime during KGO -> KO upgrades and in upgrades between KO versions.
  [#2317](https://github.com/Kong/kong-operator/pull/2317)

## [v2.0.1]

> Release date: 2025-09-17

### Fixes

- Fix incorrect error handling during cluster CA secret creation.
  [#2250](https://github.com/Kong/kong-operator/pull/2250)
- `DataPlane` is now marked as ready when `status.AvailableReplicas` is at least equal to `status.Replicas`.
  [#2291](https://github.com/Kong/kong-operator/pull/2291)

## [v2.0.0]

> Release date: 2025-09-09

> KGO becomes KO, which stands for Kong Operator. Kubernetes Gateway Operator and Kubernetes Ingress Controller
> become a single product. Furthermore, Kong Operator provides all features that used to be reserved for the
> Enterprise flavor of Kong Gateway Operator.

### Breaking Changes

- `KonnectExtension` has been bumped to `v1alpha2` and the Control plane reference via plain `KonnectID`
  has been removed. `Mirror` `GatewayControlPlane` resource is now the only way to reference remote
  control planes in read-only.
  [#1711](https://github.com/kong/kong-operator/pull/1711)
- Rename product from Kong Gateway Operator to Kong Operator.
  [#1767](https://github.com/Kong/kong-operator/pull/1767)
- Add `--cluster-domain` flag and set default to `'cluster.local'`
  This commit introduces a new `--cluster-domain` flag to the KO binary, which is now propagated to the ingress-controller.
  The default value for the cluster domain is set to `'cluster.local'`, whereas previously it was an empty string (`''`).
  This is a breaking change, as any code or configuration relying on the previous default will now use `'cluster.local'`
  unless explicitly overridden.
  [#1870](https://github.com/Kong/kong-operator/pull/1870)
- Introduce `ControlPlane` in version `v2alpha1`
  - Usage of the last valid config for fallback configuration is enabled by default,
    can be adjusted in the `spec.translation.fallbackConfiguration.useLastValidConfig` field.
    [#1939](https://github.com/Kong/kong-operator/issues/1939)
- `ControlPlane` `v2alpha1` has been replaced by `ControlPlane` `v2beta1`.
  `GatewayConfiguration` `v2alpha1` has been replaced by `GatewayConfiguration` `v2beta1`.
  [#2008](https://github.com/Kong/kong-operator/pull/2008)
- Add flags `--secret-label-selector` and `--config-map-label-selector` to
  filter watched `Secret`s and `ConfigMap`s. Only secrets or configMaps with
  the given label to `true` are reconciled by the controllers.
  For example, if `--secret-label-selector` is set to `konghq.com/secret`,
  only `Secret`s with the label `konghq.com/secret=true` are reconciled.
  The default value of the two labels are set to `konghq.com/secret` and
  `konghq.com/configmap`.
  [#1922](https://github.com/Kong/kong-operator/pull/1922)
- `GatewayConfiguration` `v1beta1` has been replaced by the new API version `v2alpha1`.
  The `GatewayConfiguration` `v1beta1` is still available but has been marked as
  deprecated.
  [#1792](https://github.com/Kong/kong-operator/pull/1972)
- Removed `KongIngress`, `TCPIngress` and `UDPIngress` CRDs together with their controllers.
  For migration guidance from these resources to Gateway API, please refer to the
  [migration documentation](https://developer.konghq.com/kubernetes-ingress-controller/migrate/ingress-to-gateway/).
  [#1971](https://github.com/Kong/kong-operator/pull/1971)
- Change env vars prefix from `GATEWAY_OPERATOR_` to `KONG_OPERATOR_`.
  `GATEWAY_OPERATOR_` prefixed env vars are still accepted but reported as deprecated.
  [#2004](https://github.com/Kong/kong-operator/pull/2004)

### Added

- Support for `cert-manager` certificate provisioning for webhooks in Helm Chart.
  [#2122](https://github.com/Kong/kong-operator/pull/2122)
- Support specifying labels to filter watched `Secret`s and `ConfigMap`s of
  each `ControlPlane` by `spec.objectFilters.secrets.matchLabels` and
  `spec.objectFilters.configMaps.matchLabels`. Only secrets or configmaps that
  have the labels matching the specified labels in spec are reconciled.
  If Kong operator has also flags `--secret-label-selector` or
  `--config-map-label-selector` set, the controller for each `ControlPlane` also
  requires reconciled secrets or configmaps to set the labels given in the flags
  to `true`.
  [#1982](https://github.com/Kong/kong-operator/pull/1982)
- Add conversion webhook for `KonnectGatewayControlPlane` to support seamless conversion
  between old `v1alpha1` and new `v1alpha2` API versions.
  [#2023](https://github.com/Kong/kong-operator/pull/2023)
- Add Konnect related configuration fields to `ControlPlane` spec, allowing fine-grained
  control over Konnect integration settings including consumer synchronization, licensing
  configuration, node refresh periods, and config upload periods.
  [#2009](https://github.com/Kong/kong-operator/pull/2009)
- Added `OptionsValid` condition to `ControlPlane`s' status. The status is set to
  `True` if the `ControlPlane`'s options in its `spec` is valid and set to `False`
  if the options are invalid against the operator's configuration.
  [#2070](https://github.com/Kong/kong-operator/pull/2070)
- Added `APIConversion` interface to bootstrap Gateway API support in Konnect hybrid
  mode.
  [#2134](https://github.com/Kong/kong-operator/pull/2134)
- Move implementation of ControlPlane Extensions mechanism and DataPlaneMetricsExtension from EE.
  [#1583](https://github.com/kong/kong-operator/pull/1583)
- Move implementation of certificate management for Konnect DPs from EE.
  [#1590](https://github.com/kong/kong-operator/pull/1590)
- `ControlPlane` status fields `controllers` and `featureGates` are filled in with
  actual configured values based on the defaults and the `spec` fields.
  [#1771](https://github.com/kong/kong-operator/pull/1771)
- Added the following CLI flags to control operator's behavior:
  - `--cache-sync-timeout` to control controller-runtime's time limit set to wait for syncing caches.
    [#1818](https://github.com/kong/kong-operator/pull/1818)
  - `--cache-sync-period` to control controller-runtime's cache sync period.
    [#1846](https://github.com/kong/kong-operator/pull/1846)
- Support the following configuration for running control plane managers in
  the `ControlPlane` CRD:
  - Specifying the delay to wait for Kubernetes object caches sync before
    updating dataplanes by `spec.cache.initSyncDuration`
    [#1858](https://github.com/Kong/kong-operator/pull/1858)
  - Specifying the period and timeout of syncing Kong configuration to dataplanes
    by `spec.dataplaneSync.interval` and `spec.dataplaneSync.timeout`
    [#1886](https://github.com/Kong/kong-operator/pull/1886)
  - Specifying the combined services from HTTPRoutes feature via
    by `spec.translation.combinedServicesFromDifferentHTTPRoutes`
    [#1934](https://github.com/Kong/kong-operator/pull/1934)
  - Specifying the drain support by `spec.translation.drainSupport`
    [#1940](https://github.com/Kong/kong-operator/pull/1940)
- Introduce flags `--apiserver-host` for API, `--apiserver-qps` and
  `--apiserver-burst` to control the QPS and burst (rate-limiting) for the
  Kubernetes API server client.
  [#1887](https://github.com/Kong/kong-operator/pull/1887)
- Introduce the flag `--emit-kubernetes-events` to enable/disable the creation of
  Kubernetes events in the `ControlPlane`. The default value is `true`.
  [#1888](https://github.com/Kong/kong-operator/pull/1888)
- Added the flag `--enable-controlplane-config-dump` to enable debug server for
  dumping Kong configuration translated from `ControlPlane`s and flag
  `--controlplane-config-dump-bind-address` to set the bind address of server.
  You can access `GET /debug/controlplanes` to list managed `ControlPlane`s and
  get response like `{"controlPlanes":[{"namespace":"default","name":"kong-12345","id":"abcd1234-..."}]}`
  listing the namespace, name and UID of managed `ControlPlane`s.
  Calling `GET /debug/controlplanes/namespace/{namespace}/name/{name}/config/{req_type}`
  can dump Kong configuration of a specific `ControlPlane`. This endpoint is
  only available when the `ControlPlane`'s `spec.configDump.state` is set to `enabled`.
  The `{req_type}` stands for the request type of dumping configuration.
  Supported `{req_type}`s are:
  - `successful` for configuration in the last successful application.
  - `failed` for configuration in the last failed application.
  - `fallback` for configuration applied in the last fallback procedure.
  - `raw-error` for raw errors returned from the dataplane in the last failed
     application.
  - `diff-report` for summaries of differences between the last applied
     configuration and the confiugration in the dataplane before that application.
     It requires the `ControlPlane` set `spec.configDump.dumpSensitive` to `enabled`.
  [#1894](https://github.com/Kong/kong-operator/pull/1894)
- Introduce the flag `--watch-namespaces` to specify which namespaces the operator
  should watch for configuration resources.
  The default value is `""` which makes the operator watch all namespaces.
  This flag is checked against the `ControlPlane`'s `spec.watchNamespaces`
  field during `ControlPlane` reconciliation and if incompatible, `ControlPlane`
  reconciliation returns with an error.
  [#1958](https://github.com/Kong/kong-operator/pull/1958)
  [#1974](https://github.com/Kong/kong-operator/pull/1974)
- Refactored Konnect extension processing for `ControlPlane` and `DataPlane` resources
  by introducing the `ExtensionProcessor` interface.
  This change enables KonnecExtensions for `ControlPlane v2alpha1`.
  [#1978](https://github.com/Kong/kong-operator/pull/1978)

### Changes

- `ControlPlane` provisioned conditions' reasons have been renamed to actually reflect
  the new operator architecture. `PodsReady` is now `Provisioned` and `PodsNotReady`
  is now `ProvisioningInProgress`.
  [#1985](https://github.com/Kong/kong-operator/pull/1985)
- Vendor gateway-operator CRDs locally and switch Kustomize to use the vendored source.
  [#2195](https://github.com/Kong/kong-operator/pull/2195)
- `kong/kong-gateway` v3.11 is the default proxy image.
  [#2212](https://github.com/Kong/kong-operator/pull/2212)

### Fixes

- Do not check "Programmed" condition in status of `Gateway` listeners in
  extracting certificates in controlplane's translation of Kong configuration.
  This fixes the disappearance of certificates when deployment status of
  `DataPlane` owned by the gateway (including deletion of pods, rolling update
  of dataplane deployment, scaling of dataplane and so on).
  [#2038](https://github.com/Kong/kong-operator/pull/2038)
- Correctly assume default Kong router flavor is `traditional_compatible` when
  `KONG_ROUTER_FLAVOR` is not set. This fixes incorrectly populated
  `GatewayClass.status.supportedFeatures` when the default was assumed to be
  `expressions`.
  [#2043](https://github.com/Kong/kong-operator/pull/2043)
- Support setting exposed nodeport of the dataplane service for `Gateway`s by
  `nodePort` field in `spec.listenersOptions`.
  [#2058](https://github.com/Kong/kong-operator/pull/2058)
- Fixed lack of `instance_name` and `protocols` reconciliation for `KongPluginBinding` when reconciling against Konnect.
  [#1681](https://github.com/kong/kong-operator/pull/1681)
- The `KonnectExtension` status is kept updated when the `KonnectGatewayControlPlane` is deleted and
  re-created. When this happens, the `KonnectGatewayControlPlane` sees its Konnect ID changed, as well
  as the endpoints. All this data is constantly enforced into the `KonnectExtension` status.
  [#1684](https://github.com/kong/kong-operator/pull/1684)
- Fix the issue that invalid label value causing ingress controller fails to
  store the license from Konnect into `Secret`.
  [#1976](https://github.com/Kong/kong-operator/pull/1976)
- Fixed a missing watch in `GatewayClass` reconciler for related `GatewayConfiguration` resources.
  [#2161](https://github.com/Kong/kong-operator/pull/2161)

## [v1.6.2]

> Release date: 2025-07-11

### Fixes

- Ignore the `ForbiddenError` in `sdk-konnect-go` returned from running CRUD
  operations against Konnect APIs. This prevents endless reconciliation when an
  operation is not allowed (due to e.g. exhausted quota).
  [#1811](https://github.com/Kong/kong-operator/pull/1811)

## [v1.6.1]

> Release date: 2025-05-22

## Changed

- Allowed the `kubectl rollout restart` operation for Deployment resources created via DataPlane CRD.
  [#1660](https://github.com/kong/kong-operator/pull/1660)

## [v1.6.0]

> Release date: 2025-05-07

### Added

- In `KonnectGatewayControlPlane` fields `Status.Endpoints.ControlPlaneEndpoint`
  and `Status.Endpoints.TelemetryEndpoint` are filled with respective values from Konnect.
  [#1415](https://github.com/kong/kong-operator/pull/1415)
- Add `namespacedRef` support for referencing networks in `KonnectCloudGatewayDataPlaneGroupConfiguration`
  [#1423](https://github.com/kong/kong-operator/pull/1423)
- Introduced new CLI flags:
  - `--logging-mode` (or `GATEWAY_OPERATOR_LOGGING_MODE` env var) to set the logging mode (`development` can be set
    for simplified logging).
  - `--validate-images` (or `GATEWAY_OPERATOR_VALIDATE_IMAGES` env var) to enable ControlPlane and DataPlane image
    validation (it's set by default to `true`).
  [#1435](https://github.com/kong/kong-operator/pull/1435)
- Add support for `-enforce-config` for `ControlPlane`'s `ValidatingWebhookConfiguration`.
  This allows to use operator's `ControlPlane` resources in AKS clusters.
  [#1512](https://github.com/kong/kong-operator/pull/1512)
- `KongRoute` can be migrated from serviceless to service bound and vice versa.
  [#1492](https://github.com/kong/kong-operator/pull/1492)
- Add `KonnectCloudGatewayTransitGateway` controller to support managing Konnect
  transit gateways.
  [#1489](https://github.com/kong/kong-operator/pull/1489)
- Added support for setting `PodDisruptionBudget` in `GatewayConfiguration`'s `DataPlane` options.
  [#1526](https://github.com/kong/kong-operator/pull/1526)
- Added `spec.watchNamespace` field to `ControlPlane` and `GatewayConfiguration` CRDs
  to allow watching resources only in the specified namespace.
  When `spec.watchNamespace.type=list` is used, each specified namespace requires
  a `WatchNamespaceGrant` that allows the `ControlPlane` to watch resources in the specified namespace.
  Aforementioned list is extended with `ControlPlane`'s own namespace which doesn't
  require said `WatchNamespaceGrant`.
  [#1388](https://github.com/kong/kong-operator/pull/1388)
  [#1410](https://github.com/kong/kong-operator/pull/1410)
  [#1555](https://github.com/kong/kong-operator/pull/1555)
  For more information on this please see: https://developer.konghq.com/operator/reference/control-plane-watch-namespaces/#controlplane-s-watchnamespaces-field
- Implemented `Mirror` and `Origin` `KonnectGatewayControlPlane`s.
  [#1496](https://github.com/kong/kong-operator/pull/1496)

### Changes

- Deduce `KonnectCloudGatewayDataPlaneGroupConfiguration` region based on the attached
  `KonnectAPIAuthConfiguration` instead of using a hardcoded `eu` value.
  [#1409](https://github.com/kong/kong-operator/pull/1409)
- Support `NodePort` as ingress service type for `DataPlane`
  [#1430](https://github.com/kong/kong-operator/pull/1430)
- Allow setting `NodePort` port number for ingress service for `DataPlane`.
  [#1516](https://github.com/kong/kong-operator/pull/1516)
- Updated `kubernetes-configuration` dependency for adding `scale` subresource for `DataPlane` CRD.
  [#1523](https://github.com/kong/kong-operator/pull/1523)
- Bump `kong/kubernetes-configuration` dependency to v1.4.0
  [#1574](https://github.com/kong/kong-operator/pull/1574)

### Fixes

- Fix setting the defaults for `GatewayConfiguration`'s `ReadinessProbe` when only
  timeouts and/or delays are specified. Now the HTTPGet field is set to `/status/ready`
  as expected with the `Gateway` scenario.
  [#1395](https://github.com/kong/kong-operator/pull/1395)
- Fix ingress service name not being applied when using `GatewayConfiguration`.
  [#1515](https://github.com/kong/kong-operator/pull/1515)
- Fix ingress service port name setting.
  [#1524](https://github.com/kong/kong-operator/pull/1524)

## [v1.5.1]

> Release date: 2025-04-01

### Added

- Add `namespacedRef` support for referencing networks in `KonnectCloudGatewayDataPlaneGroupConfiguration`
  [#1425](https://github.com/kong/kong-operator/pull/1425)
- Set `ControlPlaneRefValid` condition to false when reference to `KonnectGatewayControlPlane` is invalid
  [#1421](https://github.com/kong/kong-operator/pull/1421)

### Changes

- Deduce `KonnectCloudGatewayDataPlaneGroupConfiguration` region based on the attached
  `KonnectAPIAuthConfiguration` instead of using a hardcoded `eu` value.
  [#1417](https://github.com/kong/kong-operator/pull/1417)
- Bump `kong/kubernetes-configuration` dependency to v1.3.

## [v1.5.0]

> Release date: 2025-03-11

### Breaking Changes

- Added check of whether using `Secret` in another namespace in `AIGateway`'s
  `spec.cloudProviderCredentials` is allowed. If the `AIGateway` and the `Secret`
  referenced in `spec.cloudProviderCredentials` are not in the same namespace,
  there MUST be a `ReferenceGrant` in the namespace of the `Secret` that allows
  the `AIGateway`s to reference the `Secret`.
  This may break usage of `AIGateway`s that is already using `Secret` in
  other namespaces as AI cloud provider credentials.
  [#1161](https://github.com/kong/kong-operator/pull/1161)
- Migrate KGO CRDs to the kubernetes-configuration repo.
  With this migration process, we have removed the `api` and `pkg/clientset` from the KGO repo.
  This is a breaking change which requires manual action for projects that use operator's Go APIs.
  In order to migrate please use the import paths from the [kong/kubernetes-configuration][kubernetes-configuration] repo instead.
  For example:
  `github.com/kong/kong-operator/api/v1beta1` becomes
  `github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1`.
  [#1148](https://github.com/kong/kong-operator/pull/1148)
- Support for the `konnect-extension.gateway-operator.konghq.com` CRD has been interrupted. The new
  API `konnect-extension.konnect.konghq.com` must be used instead. The migration path is described in
  the [Kong documentation](https://developer.konghq.com/operator/konnect/reference/migrate-1.4-1.5/).
  [#1183](https://github.com/kong/kong-operator/pull/1183)
- Migrate KGO CRDs conditions to the kubernetes-configuration repo.
  With this migration process, we have moved all conditions from the KGO repo to [kubernetes-configuration](kubernetes-configuration).
  This is a breaking change which requires manual action for projects that use operator's Go conditions types.
  In order to migrate please use the import paths from the [kong/kubernetes-configuration][kubernetes-configuration] repo instead.
  [#1281](https://github.com/kong/kong-operator/pull/1281)
  [#1305](https://github.com/kong/kong-operator/pull/1305)
  [#1306](https://github.com/kong/kong-operator/pull/1306)
  [#1318](https://github.com/kong/kong-operator/pull/1318)

[kubernetes-configuration]: https://github.com/Kong/kubernetes-configuration

### Added

- Added `Name` field in `ServiceOptions` to allow specifying name of the
  owning service. Currently specifying ingress service of `DataPlane` is
  supported.
  [#966](https://github.com/kong/kong-operator/pull/966)
- Added support for global plugins with `KongPluginBinding`'s `scope` field.
  The default value is `OnlyTargets` which means that the plugin will be
  applied only to the targets specified in the `targets` field. The new
  alternative is `GlobalInControlPlane` that will make the plugin apply
  globally in a control plane.
  [#1052](https://github.com/kong/kong-operator/pull/1052)
- Added `-cluster-ca-key-type` and `-cluster-ca-key-size` CLI flags to allow
  configuring cluster CA private key type and size. Currently allowed values:
  `rsa` and `ecdsa` (default).
  [#1081](https://github.com/kong/kong-operator/pull/1081)
- The `GatewayClass` Accepted Condition is set to `False` with reason `InvalidParameters`
  in case the `.spec.parametersRef` field is not a valid reference to an existing
  `GatewayConfiguration` object.
  [#1021](https://github.com/kong/kong-operator/pull/1021)
- The `SupportedFeatures` field is properly set in the `GatewayClass` status.
  It requires the experimental version of Gateway API (as of v1.2.x) installed in
  your cluster, and the flag `--enable-gateway-api-experimental` set.
  [#1010](https://github.com/kong/kong-operator/pull/1010)
- Added support for `KongConsumer` `credentials` in Konnect entities support.
  Users can now specify credentials for `KongConsumer`s in `Secret`s and reference
  them in `KongConsumer`s' `credentials` field.
  - `basic-auth` [#1120](https://github.com/kong/kong-operator/pull/1120)
  - `key-auth` [#1168](https://github.com/kong/kong-operator/pull/1168)
  - `acl` [#1187](https://github.com/kong/kong-operator/pull/1187)
  - `jwt` [#1208](https://github.com/kong/kong-operator/pull/1208)
  - `hmac` [#1222](https://github.com/kong/kong-operator/pull/1222)
- Added prometheus metrics for Konnect entity operations in the metrics server:
  - `gateway_operator_konnect_entity_operation_count` for number of operations.
  - `gateway_operator_konnect_entity_operation_duration_milliseconds` for duration of operations.
  [#953](https://github.com/kong/kong-operator/pull/953)
- Added support for `KonnectCloudGatewayNetwork` CRD which can manage Konnect
  Cloud Gateway Network entities.
  [#1136](https://github.com/kong/kong-operator/pull/1136)
- Reconcile affected `KonnectExtension`s when the `Secret` used as Dataplane
  certificate is modified. A secret must have the `konghq.com/konnect-dp-cert`
  label to trigger the reconciliation.
  [#1250](https://github.com/kong/kong-operator/pull/1250)
- When the `DataPlane` is configured in Konnect, the `/status/ready` endpoint
  is set as the readiness probe.
  [#1235](https://github.com/kong/kong-operator/pull/1253)
- Added support for `KonnectDataPlaneGroupConfiguration` CRD which can manage Konnect
  Cloud Gateway DataPlane Group configurations entities.
  [#1186](https://github.com/kong/kong-operator/pull/1186)
- Supported `KonnectExtension` to attach to Konnect control planes by setting
  namespace and name of `KonnectGatewayControlPlane` in `spec.konnectControlPlane`.
  [#1254](https://github.com/kong/kong-operator/pull/1254)
- Added support for `KonnectExtension`s on `ControlPlane`s.
  [#1262](https://github.com/kong/kong-operator/pull/1262)
- Added support for `KonnectExtension`'s `status` `controlPlaneRefs` and `dataPlaneRefs`
  fields.
  [#1297](https://github.com/kong/kong-operator/pull/1297)
- Added support for `KonnectExtension`s on `Gateway`s via `GatewayConfiguration`
  extensibility.
  [#1292](https://github.com/kong/kong-operator/pull/1292)
- Added `-enforce-config` flag to enforce the configuration of the `ControlPlane`
  and `DataPlane` `Deployment`s.
  [#1307](https://github.com/kong/kong-operator/pull/1307)
- Added Automatic secret provisioning for `KonnectExtension` certificates.
  [#1304](https://github.com/kong/kong-operator/pull/1304)

### Changed

- `KonnectExtension` does not require `spec.serverHostname` to be set by a user
  anymore - default is set to `konghq.com`.
  [#947](https://github.com/kong/kong-operator/pull/947)
- Support KIC 3.4
  [#972](https://github.com/kong/kong-operator/pull/972)
- Allow more than 1 replica for `ControlPlane`'s `Deployment` to support HA deployments of KIC.
  [#978](https://github.com/kong/kong-operator/pull/978)
- Removed support for the migration of legacy labels so upgrading the operator from 1.3 (or older) to 1.5.0,
  should be done through 1.4.1
  [#976](https://github.com/kong/kong-operator/pull/976)
- Move `ControlPlane` `image` validation to CRD CEL rules.
  [#984](https://github.com/kong/kong-operator/pull/984)
- Remove usage of `kube-rbac-proxy`.
  Its functionality of can be now achieved by using the new flag `--metrics-access-filter`
  (or a corresponding `GATEWAY_OPERATOR_METRICS_ACCESS_FILTER` env).
  The default value for the flag is `off` which doesn't restrict the access to the metrics
  endpoint. The flag can be set to `rbac` which will configure KGO to verify the token
  sent with the request.
  For more information on this migration please consult
  [kubernetes-sigs/kubebuilder#3907][kubebuilder_3907].
  [#956](https://github.com/kong/kong-operator/pull/956)
- Move `DataPlane` ports validation to `ValidationAdmissionPolicy` and `ValidationAdmissionPolicyBinding`.
  [#1007](https://github.com/kong/kong-operator/pull/1007)
- Move `DataPlane` db mode validation to CRD CEL validation expressions.
  With this change only the `KONG_DATABASE` environment variable directly set in
  the `podTemplateSpec` is validated. `EnvFrom` is not evaluated anymore for this validation.
  [#1049](https://github.com/kong/kong-operator/pull/1049)
- Move `DataPlane` promotion in progress validation to CRD CEL validation expressions.
  This is relevant for `DataPlane`s with BlueGreen rollouts enabled only.
  [#1054](https://github.com/kong/kong-operator/pull/1054)
- Move `DataPlane`'s rollout strategy validation of disallowed `AutomaticPromotion`
  to CRD CEL validation expressions.
  This is relevant for `DataPlane`s with BlueGreen rollouts enabled only.
  [#1056](https://github.com/kong/kong-operator/pull/1056)
- Move `DataPlane`'s rollout resource strategy validation of disallowed `DeleteOnPromotionRecreateOnRollout`
  to CRD CEL validation expressions.
  This is relevant for `DataPlane`s with BlueGreen rollouts enabled only.
  [#1065](https://github.com/kong/kong-operator/pull/1065)
- The `GatewayClass` Accepted Condition is set to `False` with reason `InvalidParameters`
  in case the `.spec.parametersRef` field is not a valid reference to an existing
  `GatewayConfiguration` object.
  [#1021](https://github.com/kong/kong-operator/pull/1021)
- Validating webhook is now disabled by default. At this point webhook doesn't
  perform any validations.
  These were all moved either to CRD CEL validation expressions or to the
  `ValidationAdmissionPolicy`.
  Flag remains in place to not cause a breaking change for users that rely on it.
  [#1066](https://github.com/kong/kong-operator/pull/1066)
- Remove `ValidatingAdmissionWebhook` from the operator.
  As of now, all the validations have been moved to CRD CEL validation expressions
  or to the `ValidationAdmissionPolicy`.
  All the flags that were configuring the webhook are now deprecated and do not
  have any effect.
  They will be removed in next major release.
  [#1100](https://github.com/kong/kong-operator/pull/1100)
- Konnect entities that are attached to a Konnect CP through a `ControlPlaneRef`
  do not get an owner relationship set to the `ControlPlane` anymore hence
  they are not deleted when the `ControlPlane` is deleted.
  [#1099](https://github.com/kong/kong-operator/pull/1099)
- Remove the owner relationship between `KongService` and `KongRoute`.
  [#1178](https://github.com/kong/kong-operator/pull/1178)
- Remove the owner relationship between `KongTarget` and `KongUpstream`.
  [#1279](https://github.com/kong/kong-operator/pull/1279)
- Remove the owner relationship between `KongCertificate` and `KongSNI`.
  [#1285](https://github.com/kong/kong-operator/pull/1285)
- Remove the owner relationship between `KongKey`s and `KongKeysSet`s and `KonnectGatewayControlPlane`s.
  [#1291](https://github.com/kong/kong-operator/pull/1291)
- Check whether an error from calling Konnect API is a validation error by
  HTTP status code in Konnect entity controller. If the HTTP status code is
  `400`, we consider the error as a validation error and do not try to requeue
  the Konnect entity.
  [#1226](https://github.com/kong/kong-operator/pull/1226)
- Credential resources used as Konnect entities that are attached to a `KongConsumer`
  resource do not get an owner relationship set to the `KongConsumer` anymore hence
  they are not deleted when the `KongConsumer` is deleted.
  [#1259](https://github.com/kong/kong-operator/pull/1259)

[kubebuilder_3907]: https://github.com/kubernetes-sigs/kubebuilder/discussions/3907

### Fixes

- Fix `DataPlane`s with `KonnectExtension` and `BlueGreen` settings. Both the Live
  and preview deployments are now customized with Konnect-related settings.
  [#910](https://github.com/kong/kong-operator/pull/910)
- Remove `RunAsUser` specification in jobs to create webhook certificates
  because Openshift does not specifying `RunAsUser` by default.
  [#964](https://github.com/kong/kong-operator/pull/964)
- Fix watch predicates for types shared between KGO and KIC.
  [#948](https://github.com/kong/kong-operator/pull/948)
- Fix unexpected error logs caused by passing an odd number of arguments to the logger
  in the `KongConsumer` reconciler.
  [#983](https://github.com/kong/kong-operator/pull/983)
- Fix checking status when using a `KonnectGatewayControlPlane` with KIC CP type
  as a `ControlPlaneRef`.
  [#1115](https://github.com/kong/kong-operator/pull/1115)
- Fix setting `DataPlane`'s readiness probe using `GatewayConfiguration`.
  [#1118](https://github.com/kong/kong-operator/pull/1118)
- Fix handling Konnect API conflicts.
  [#1176](https://github.com/kong/kong-operator/pull/1176)

## [v1.4.2]

> Release date: 2025-01-23

### Fixes

- Bump `kong/kubernetes-configuration` dependency to v1.0.8 that fixes the issue with `spec.headers`
  in `KongRoute` CRD by aligning to the expected schema (instead of `map[string]string`, it should be
  `map[string][]string`).
  Please make sure you update the KGO channel CRDs accordingly in your cluster:
  `kustomize build github.com/Kong/kubernetes-configuration/config/crd/gateway-operator\?ref=v1.0.8 | kubectl apply -f -`
  [#1072](https://github.com/kong/kong-operator/pull/1072)

## [v1.4.1]

> Release date: 2024-11-28

### Fixes

- Fix setting the `ServiceAccountName` for `DataPlane`'s `Deployment`.
  [#897](https://github.com/kong/kong-operator/pull/897)
- Fixed setting `ExternalTrafficPolicy` on `DataPlane`'s ingress `Service` when
  the requested value is empty.
  [#898](https://github.com/kong/kong-operator/pull/898)
- Set 0 members on `KonnectGatewayControlPlane` which type is set to group.
  [#896](https://github.com/kong/kong-operator/pull/896)
- Fixed a `panic` in `KonnectAPIAuthConfigurationReconciler` occuring when nil
  response was returned by Konnect API when fetching the organization information.
  [#901](https://github.com/kong/kong-operator/pull/901)
- Bump sdk-konnect-go version to 0.1.10 to fix handling global API endpoints.
  [#894](https://github.com/kong/kong-operator/pull/894)

## [v1.4.0]

> Release date: 2024-10-31

### Added

- Proper `User-Agent` header is now set on outgoing HTTP requests.
  [#387](https://github.com/kong/kong-operator/pull/387)
- Introduce `KongPluginInstallation` CRD to allow installing custom Kong
  plugins distributed as container images.
  [#400](https://github.com/kong/kong-operator/pull/400), [#424](https://github.com/kong/kong-operator/pull/424), [#474](https://github.com/kong/kong-operator/pull/474), [#560](https://github.com/kong/kong-operator/pull/560), [#615](https://github.com/kong/kong-operator/pull/615), [#476](https://github.com/kong/kong-operator/pull/476)
- Extended `DataPlane` API with a possibility to specify `PodDisruptionBudget` to be
  created for the `DataPlane` deployments via `spec.resources.podDisruptionBudget`.
  [#464](https://github.com/kong/kong-operator/pull/464)
- Add `KonnectAPIAuthConfiguration` reconciler.
  [#456](https://github.com/kong/kong-operator/pull/456)
- Add support for Konnect tokens in `Secrets` in `KonnectAPIAuthConfiguration`
  reconciler.
  [#459](https://github.com/kong/kong-operator/pull/459)
- Add `KonnectControlPlane` reconciler.
  [#462](https://github.com/kong/kong-operator/pull/462)
- Add `KongService` reconciler for Konnect control planes.
  [#470](https://github.com/kong/kong-operator/pull/470)
- Add `KongUpstream` reconciler for Konnect control planes.
  [#593](https://github.com/kong/kong-operator/pull/593)
- Add `KongConsumer` reconciler for Konnect control planes.
  [#493](https://github.com/kong/kong-operator/pull/493)
- Add `KongRoute` reconciler for Konnect control planes.
  [#506](https://github.com/kong/kong-operator/pull/506)
- Add `KongConsumerGroup` reconciler for Konnect control planes.
  [#510](https://github.com/kong/kong-operator/pull/510)
- Add `KongCACertificate` reconciler for Konnect CA certificates.
  [#626](https://github.com/kong/kong-operator/pull/626)
- Add `KongCertificate` reconciler for Konnect Certificates.
  [#643](https://github.com/kong/kong-operator/pull/643)
- Added command line flags to configure the certificate generator job's images.
  [#516](https://github.com/kong/kong-operator/pull/516)
- Add `KongPluginBinding` reconciler for Konnect Plugins.
  [#513](https://github.com/kong/kong-operator/pull/513), [#535](https://github.com/kong/kong-operator/pull/535)
- Add `KongTarget` reconciler for Konnect Targets.
  [#627](https://github.com/kong/kong-operator/pull/627)
- Add `KongVault` reconciler for Konnect Vaults.
  [#597](https://github.com/kong/kong-operator/pull/597)
- Add `KongKey` reconciler for Konnect Keys.
  [#646](https://github.com/kong/kong-operator/pull/646)
- Add `KongKeySet` reconciler for Konnect KeySets.
  [#657](https://github.com/kong/kong-operator/pull/657)
- Add `KongDataPlaneClientCertificate` reconciler for Konnect DataPlaneClientCertificates.
  [#694](https://github.com/kong/kong-operator/pull/694)
- The `KonnectExtension` CRD has been introduced. Such a CRD can be attached
  to a `DataPlane` via the extensions field to have a konnect-flavored `DataPlane`.
  [#453](https://github.com/kong/kong-operator/pull/453),
  [#578](https://github.com/kong/kong-operator/pull/578),
  [#736](https://github.com/kong/kong-operator/pull/736)
- Entities created in Konnect are now labeled (or tagged for those that does not
  support labels) with origin Kubernetes object's metadata: `k8s-name`, `k8s-namespace`,
  `k8s-uid`, `k8s-generation`, `k8s-kind`, `k8s-group`, `k8s-version`.
  [#565](https://github.com/kong/kong-operator/pull/565)
- Add `KongService`, `KongRoute`, `KongConsumer`, and `KongConsumerGroup` watchers
  in the `KongPluginBinding` reconciler.
  [#571](https://github.com/kong/kong-operator/pull/571)
- Annotating the following resource with the `konghq.com/plugins` annotation results in
  the creation of a managed `KongPluginBinding` resource:
  - `KongService` [#550](https://github.com/kong/kong-operator/pull/550)
  - `KongRoute` [#644](https://github.com/kong/kong-operator/pull/644)
  - `KongConsumer` [#676](https://github.com/kong/kong-operator/pull/676)
  - `KongConsumerGroup` [#684](https://github.com/kong/kong-operator/pull/684)
  These `KongPluginBinding`s are taken by the `KongPluginBinding` reconciler
  to create the corresponding plugin objects in Konnect.
- `KongConsumer` associated with `ConsumerGroups` is now reconciled in Konnect by removing/adding
  the consumer from/to the consumer groups.
  [#592](https://github.com/kong/kong-operator/pull/592)
- Add support for `KongConsumer` credentials:
  - basic-auth [#625](https://github.com/kong/kong-operator/pull/625)
  - API key [#635](https://github.com/kong/kong-operator/pull/635)
  - ACL [#661](https://github.com/kong/kong-operator/pull/661)
  - JWT [#678](https://github.com/kong/kong-operator/pull/678)
  - HMAC Auth [#687](https://github.com/kong/kong-operator/pull/687)
- Add support for `KongRoute`s bound directly to `KonnectGatewayControlPlane`s (serviceless rotues).
  [#669](https://github.com/kong/kong-operator/pull/669)
- Allow setting `KonnectGatewayControlPlane`s group membership
  [#697](https://github.com/kong/kong-operator/pull/697)
- Apply Konnect-related customizations to `DataPlane`s that properly reference `KonnectExtension`
  resources.
  [#714](https://github.com/kong/kong-operator/pull/714)
- The KonnectExtension functionality is enabled only when the `--enable-controller-konnect`
  flag or the `GATEWAY_OPERATOR_ENABLE_CONTROLLER_KONNECT` env var is set.
  [#738](https://github.com/kong/kong-operator/pull/738)

### Fixes

- Fixed `ControlPlane` cluster wide resources not migrating to new ownership labels
  (introduced in 1.3.0) when upgrading the operator from 1.2 (or older) to 1.3.0.
  [#369](https://github.com/kong/kong-operator/pull/369)
- Requeue instead of reporting an error when a finalizer removal yields a conflict.
  [#454](https://github.com/kong/kong-operator/pull/454)
- Requeue instead of reporting an error when a GatewayClass status update yields a conflict.
  [#612](https://github.com/kong/kong-operator/pull/612)
- Guard object counters with checks whether CRDs for them exist
  [#710](https://github.com/kong/kong-operator/pull/710)
- Do not reconcile Gateways nor assign any finalizers when the referred GatewayClass is not supported.
  [#711](https://github.com/kong/kong-operator/pull/711)
- Fixed setting `ExternalTrafficPolicy` on `DataPlane`'s ingress `Service` during update and patch operations.
  [#750](https://github.com/kong/kong-operator/pull/750)
- Fixed setting `ExternalTrafficPolicy` on `DataPlane`'s ingress `Service`.
  Remove the default value (`Cluster`). Prevent setting this field for `ClusterIP` `Service`s.
  [#812](https://github.com/kong/kong-operator/pull/812)

### Changes

- Default version of `ControlPlane` is bumped to 3.3.1
  [#580](https://github.com/kong/kong-operator/pull/580)
- Default version of `DataPlane` is bumped to 3.8.0
  [#572](https://github.com/kong/kong-operator/pull/572)
- Gateway API has been bumped to v1.2.0
  [#674](https://github.com/kong/kong-operator/pull/674)

## [v1.3.0]

> Release date: 2024-06-24

### Added

- Add `ExternalTrafficPolicy` to `DataPlane`'s `ServiceOptions`
  [#241](https://github.com/kong/kong-operator/pull/241)

### Breaking Changes

- Changes project layout to match `kubebuilder` `v4`. Some import paths (due to dir renames) have changed
  `apis` -> `api` and `controllers` -> `controller`.
  [#84](https://github.com/kong/kong-operator/pull/84)

### Changes

- `Gateway` do not have their `Ready` status condition set anymore.
  This aligns with Gateway API and its conformance test suite.
  [#246](https://github.com/kong/kong-operator/pull/246)
- `Gateway`s' listeners now have their `attachedRoutes` count filled in in status.
  [#251](https://github.com/kong/kong-operator/pull/251)
- Detect when `ControlPlane` has its admission webhook disabled via
  `CONTROLLER_ADMISSION_WEBHOOK_LISTEN` environment variable and ensure that
  relevant webhook resources are not created/deleted.
  [#326](https://github.com/kong/kong-operator/pull/326)
- The `OwnerReferences` on cluster-wide resources to indicate their owner are now
  replaced by a proper set of labels to identify `kind`, `namespace`, and
  `name` of the owning object.
  [#259](https://github.com/kong/kong-operator/pull/259)
- Default version of `ControlPlane` is bumped to 3.2.0
  [#327](https://github.com/kong/kong-operator/pull/327)

### Fixes

- Fix enforcing up to date `ControlPlane`'s `ValidatingWebhookConfiguration`
  [#225](https://github.com/kong/kong-operator/pull/225)

## [v1.2.3]

### Fixes

> Release date: 2024-04-23

- Fixes an issue where managed `Gateway`s controller wasn't able to reduce
  the created `DataPlane` objects when too many have been created.
  [#43](https://github.com/kong/kong-operator/pull/43)
- `Gateway` controller will no longer set `DataPlane` deployment's replicas
  to default value when `DataPlaneOptions` in `GatewayConfiguration` define
  scaling strategy. This effectively allows users to use `DataPlane` horizontal
  autoscaling with `GatewayConfiguration` as the generated `DataPlane` deployment
  will no longer be rejected.
  [#79](https://github.com/kong/kong-operator/pull/79)
- Make creating a `DataPlane` index conditional based on enabling the `ControlPlane`
  controller. This allows running KGO without `ControlPlane` CRD with its controller
  disabled.
  [#103](https://github.com/kong/kong-operator/pull/103)

## [v1.2.2]

> Release date: 2024-04-23

### **NOTE: Retracted**

[v1.2.2][rel_122] was retracted due to a misplaced git tag.
Due to [golang proxy caching modules indefinitely][goproxy] we needed to retract this version.
[v1.2.3][rel_123] contains all the changes that v1.2.2 intended to contain.

[goproxy]: https://sum.golang.org/#faq-retract-version
[rel_122]: https://github.com/kong/kong-operator/releases/tag/v1.2.2
[rel_123]: https://github.com/kong/kong-operator/releases/tag/v1.2.3

## [v1.2.1]

> Release date: 2024-03-19

### Fixes

- Fixed an issue where operator wasn't able to update `ControlPlane` `ClusterRole` or `ClusterRoleBinding`
  when they got out of date.
  [#11](https://github.com/kong/kong-operator/pull/11)

### Changes

- KGO now uses `GATEWAY_OPERATOR_` prefix for all flags, including the `zap` related logging flags.
  This means that the following can now be set:
  - `-zap-devel` (env: `GATEWAY_OPERATOR_ZAP_DEVEL`)
  - `-zap-encoder` (env: `GATEWAY_OPERATOR_ZAP_ENCODER`)
  - `-zap-log-level` (env: `GATEWAY_OPERATOR_ZAP_LOG_LEVEL`)
  - `-zap-stacktrace-level` (env: `GATEWAY_OPERATOR_ZAP_STACKTRACE_LEVEL`)
  - `-zap-time-encoding` (env: `GATEWAY_OPERATOR_ZAP_TIME_ENCODING`)

  For more details about those please consult [`zap.Options` pkg.go.dev][zap_bindflags]

[zap_bindflags]: https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.2/pkg/log/zap#Options.BindFlags

## [v1.2.0]

> Release date: 2024-03-15

## Highlights

- 🎓 The Managed `Gateway`s feature is now GA.
- 🎓 `ControlPlane` and `GatewayConfig` APIs have been promoted to `v1beta1`.
- ✨ `DataPlane`s managed by `Gateway`s can be now scaled horizontally through the
  `GatewayConfiguration` API.
- ✨ `Gateway` listeners are dynamically mapped to the `DataPlane` proxy service ports.
- 🧠 The new feature `AIGateway` has been released in `alpha` stage.

## Added

- Added support for specifying command line flags through environment
  variables having the `GATEWAY_OPERATOR_` prefix. For example, you can specify the
  value of flag `--controller-name` through the environment variable `GATEWAY_OPERATOR_CONTROLLER_NAME`.
  [kong/kong-operator-archive#1616](https://github.com/kong/kong-operator-archive/pull/1616)
- Add horizontal autoscaling for `DataPlane`s using its `scaling.horizontal` spec
  field.
  [kong/kong-operator-archive#1281](https://github.com/kong/kong-operator-archive/pull/1281)
- `ControlPlane`s now use Gateway Discovery by default, with Service DNS Strategy.
  Additionally, the `DataPlane` readiness probe has been changed to `/status/ready`
  when the `DataPlane` is managed by a `Gateway`.
  [kong/kong-operator-archive#1261](https://github.com/kong/kong-operator-archive/pull/1261)
- `Gateway`s and `Listener`s `Accepted` and `Conflicted` conditions are now set
  and enforced based on the Gateway API specifications.
  [kong/kong-operator-archive#1398](https://github.com/kong/kong-operator-archive/pull/1398)
- `ControlPlane` `ClusterRole`s and `ClusterRoleBinding`s are enforced and kept
  up to date by the `ControlPlane` controller.
  [kong/kong-operator-archive#1259](https://github.com/kong/kong-operator-archive/pull/1259)
- The `Gateway` listeners are now dynamically mapped to `DataPlane` ingress service
  ports. This means that the change of a `Gateway` spec leads to a `DataPlane` reconfiguration,
  along with an ingress service update.
  [kong/kong-operator-archive#1363](https://github.com/kong/kong-operator-archive/pull/1363)
- `--enable-controller-gateway` and `--enable-controller-controlplane` command
  line flags are set to `true` by default to enable controllers for `Gateway`s
  and `ControlPlane`s.
  [kong/kong-operator-archive#1519](https://github.com/kong/kong-operator-archive/pull/1519)
- When the `Gateway` controller provisions a `ControlPlane`, it sets the `CONTROLLER_GATEWAY_TO_RECONCILE`
  env variable to let the `ControlPlane` reconcile
  that specific `Gateway` only.
  [kong/kong-operator-archive#1529](https://github.com/kong/kong-operator-archive/pull/1529)
- `ControlPlane` is now deployed with a validating webhook server turned on. This
  involves creating `ValidatingWebhookConfiguration`, a `Service` that exposes the
  webhook and a `Secret` that holds a TLS certificate. The `Secret` is mounted in
  the `ControlPlane`'s `Pod` for the webhook server to use it.
  [kong/kong-operator-archive#1539](https://github.com/kong/kong-operator-archive/pull/1539)
  [kong/kong-operator-archive#1545](https://github.com/kong/kong-operator-archive/pull/1545)
- Added `konnectCertificate` field to the DataPlane resource.
  [kong/kong-operator-archive#1517](https://github.com/kong/kong-operator-archive/pull/1517)
- Added `v1alpha1.AIGateway` as an experimental API. This can be enabled by
  manually deploying the `AIGateway` CRD and enabling the feature on the
  controller manager with the `--enable-controller-aigateway` flag.
  [kong/kong-operator-archive#1399](https://github.com/kong/kong-operator-archive/pull/1399)
  [kong/kong-operator-archive#1542](https://github.com/kong/kong-operator-archive/pull/1399)
- Added validation on checking if ports in `KONG_PORT_MAPS` and `KONG_PROXY_LISTEN`
  environment variables of deployment options in `DataPlane` match the `ports`
  in the ingress service options of the `DataPlane`.
  [kong/kong-operator-archive#1521](https://github.com/kong/kong-operator-archive/pull/1521)

### Changes

- The `GatewayConfiguration` API has been promoted from `v1alpha1` to `v1beta1`.
  [kong/kong-operator-archive#1514](https://github.com/kong/kong-operator-archive/pull/1514)
- The `ControlPlane` API has been promoted from `v1alpha1` to `v1beta1`.
  [kong/kong-operator-archive#1523](https://github.com/kong/kong-operator-archive/pull/1523)
- The CRD's shortname of `ControlPlane` has been changed to `kocp`.
  The CRD's shortname of `DataPlane` has been changed to `kodp`.
  The CRD's shortname of `GatewayConfiguration` has been changed to `kogc`.
  [kong/kong-operator-archive#1532](https://github.com/kong/kong-operator-archive/pull/1532)
- `ControlPlane` (Kong Ingress Controller) default and minimum version has been
  bumped to 3.1.2.
  [kong/kong-operator-archive#1586](https://github.com/kong/kong-operator-archive/pull/1586)
- `DataPlane` (Kong Gateway) default version has been bumped to `v3.6.0`.
  [kong/kong-operator-archive#1577](https://github.com/kong/kong-operator-archive/pull/1577)

### Fixes

- Fixed a problem where the operator would not set the defaults to `PodTemplateSpec`
  patch and because of that it would detect a change and try to reconcile the owned
  resource where in fact the change was not there.
  One of the symptoms of this bug could have been a `StartupProbe` set in `PodSpec`
  preventing the `DataPlane` from getting correct status information.
  [kong/kong-operator-archive#1224](https://github.com/kong/kong-operator-archive/pull/1224)
- If the Gateway controller is enabled, `DataPlane` and `ControlPlane` controllers
  get enabled as well.
  [kong/kong-operator-archive#1242](https://github.com/kong/kong-operator-archive/pull/1242)
- Fix applying the `PodTemplateSpec` patch so that it's not applied when the
  calculated patch (resulting from the generated manifest and current in-cluster
  state) is empty.
  One of the symptoms of this bug was that when users tried to apply a `ReadinessProbe`
  which specified a port name instead of a number (which is what's generated by
  the operator) it would never reconcile and the status conditions would never get
  up to date `ObservedGeneration`.
  [kong/kong-operator-archive#1238](https://github.com/kong/kong-operator-archive/pull/1238)
- Fix manager RBAC permissions which prevented the operator from being able to
  create `ControlPlane`'s `ClusterRole`s, list pods or list `EndpointSlices`.
  [kong/kong-operator-archive#1255](https://github.com/kong/kong-operator-archive/pull/1255)
- `DataPlane`s with BlueGreen rollout strategy enabled will now have its Ready status
  condition updated to reflect "live" `Deployment` and `Service`s status.
  [kong/kong-operator-archive#1308](https://github.com/kong/kong-operator-archive/pull/1308)
- The `ControlPlane` `election-id` has been changed so that every `ControlPlane`
  has its own `election-id`, based on the `ControlPlane` name. This prevents `pod`s
  belonging to different `ControlPlane`s from competing for the same lease.
  [kong/kong-operator-archive#1349](https://github.com/kong/kong-operator-archive/pull/1349)
- Fill in the defaults for `env` and `volumes` when comparing the in-cluster spec
  with the generated spec.
  [kong/kong-operator-archive#1446](https://github.com/kong/kong-operator-archive/pull/1446)
- Do not flap `DataPlane`'s `Ready` status condition when e.g. ingress `Service`
  can't get an address assigned and `spec.network.services.ingress.`annotations`
  is non-empty.
  [kong/kong-operator-archive#1447](https://github.com/kong/kong-operator-archive/pull/1447)
- Update or recreate a `ClusterRoleBinding` for control planes if the existing
  one does not contain the `ServiceAccount` used by `ControlPlane`, or
  `ClusterRole` is changed.
  [kong/kong-operator-archive#1501](https://github.com/kong/kong-operator-archive/pull/1501)
- Retry reconciling `Gateway`s when provisioning owned `DataPlane` fails.
  [kong/kong-operator-archive#1553](https://github.com/kong/kong-operator-archive/pull/1553)

## [v1.1.0]

> Release date: 2023-11-20

### Added

- Add support for `ControlPlane` `v3.0` by updating the generated `ClusterRole`.
  [kong/kong-operator-archive#1189](https://github.com/kong/kong-operator-archive/pull/1189)

### Changes

- Bump `ControlPlane` default version to `v3.0`.
  [kong/kong-operator-archive#1189](https://github.com/kong/kong-operator-archive/pull/1189)
- Bump Gateway API to v1.0.
  [kong/kong-operator-archive#1189](https://github.com/kong/kong-operator-archive/pull/1189)

### Fixes

- Operator `Role` generation is fixed. As a result it contains now less rules
  hence the operator needs less permissions to run.
  [kong/kong-operator-archive#1191](https://github.com/kong/kong-operator-archive/pull/1191)

## [v1.0.3]

> Release date: 2023-11-06

### Fixes

- Fix an issue where operator is upgraded from an older version and it orphans
  old `DataPlane` resources.
  [kong/kong-operator-archive#1155](https://github.com/kong/kong-operator-archive/pull/1155)
  [kong/kong-operator-archive#1161](https://github.com/kong/kong-operator-archive/pull/1161)

### Added

- Setting `spec.deployment.podTemplateSpec.spec.volumes` and
  `spec.deployment.podTemplateSpec.spec.containers[*].volumeMounts` on `ControlPlane`s
  is now allowed.
  [kong/kong-operator-archive#1175](https://github.com/kong/kong-operator-archive/pull/1175)

## [v1.0.2]

> Release date: 2023-10-18

### Fixes

- Bump dependencies

## [v1.0.1]

> Release date: 2023-10-02

### Fixes

- Fix flapping of `Gateway` managed `ControlPlane` `spec` field when applied without
  `controlPlaneOptions` set.
  [kong/kong-operator-archive#1127](https://github.com/kong/kong-operator-archive/pull/1127)

### Changes

- Bump `ControlPlane` default version to `v2.12`.
  [kong/kong-operator-archive#1118](https://github.com/kong/kong-operator-archive/pull/1118)
- Bump `WebhookCertificateConfigBaseImage` to `v1.3.0`.
  [kong/kong-operator-archive#1130](https://github.com/kong/kong-operator-archive/pull/1130)

## [v1.0.0]

> Release date: 2023-09-26

### Changes

- Operator managed subresources are now labelled with `gateway-operator.konghq.com/managed-by`
  additionally to the old `konghq.com/gateway-operator` label.
  The value associated with this label stays the same and it still indicates the
  type of a resource that owns the subresrouce.
  The old label should not be used as it will be deleted in the future.
  [kong/kong-operator-archive#1098](https://github.com/kong/kong-operator-archive/pull/1098)
- Enable `DataPlane` Blue Green rollouts controller by default.
  [kong/kong-operator-archive#1106](https://github.com/kong/kong-operator-archive/pull/1106)

### Fixes

- Fixes handling `Volume`s and `VolumeMount`s when customizing through `DataPlane`'s
  `spec.deployment.podTemplateSpec.spec.containers[*].volumeMounts` and/or
  `spec.deployment.podTemplateSpec.spec.volumes`.
  Sample manifests are updated accordingly.
  [kong/kong-operator-archive#1095](https://github.com/kong/kong-operator-archive/pull/1095)

## [v0.7.0]

> Release date: 2023-09-13

### Added

- Added `gateway-operator.konghq.com/service-selector-override` as the dataplane
  annotation to override the default `Selector` of both the admin and proxy services.
  [kong/kong-operator-archive#921](https://github.com/kong/kong-operator-archive/pull/921)
- Added deploying of preview Admin API service when Blue Green rollout strategy
  is enabled for `DataPlane`s.
  `DataPlane`'s `status.rollout.service` is updated accordingly.
  [kong/kong-operator-archive#931](https://github.com/kong/kong-operator-archive/pull/931)
- Added `gateway-operator.konghq.com/promote-when-ready` `DataPlane` annotation to allow
  users to signal the operator should proceed with promoting the new resources when
  `BreakBeforePromotion` promotion strategy is used.
  [kong/kong-operator-archive#938](https://github.com/kong/kong-operator-archive/pull/938)
- Added deploying of preview Deployment when Blue Green rollout strategy
  is enabled for `DataPlane`s.
  [kong/kong-operator-archive#930](https://github.com/kong/kong-operator-archive/pull/930)
- Added appropriate label selectors to `DataPlane`s with enabled Blue Green rollout
  strategy. Now Admin Service and `DataPlane` Deployments correctly select their
  Pods.
  Added `DataPlane`'s `status.selector` and `status.rollout.deployment.selector` fields.
  [kong/kong-operator-archive#951](https://github.com/kong/kong-operator-archive/pull/951)
- Added setting rollout status with `RolledOut` condition
  [kong/kong-operator-archive#960](https://github.com/kong/kong-operator-archive/pull/960)
- Added deploying of preview ingress service for Blue Green rollout strategy.
  [kong/kong-operator-archive#956](https://github.com/kong/kong-operator-archive/pull/956)
- Implemented an actual promotion of a preview deployment to live state when BlueGreen
  rollout strategy is used.
  [kong/kong-operator-archive#966](https://github.com/kong/kong-operator-archive/pull/966)
- Added `PromotionFailed` condition which is set on `DataPlane`s with Blue Green
  rollout strategy when promotion related activities (like updating `DataPlane`
  service selector) fail.
  [kong/kong-operator-archive#1005](https://github.com/kong/kong-operator-archive/pull/1005)
- Added `spec.deployment.rollout.strategy.blueGreen.resources.plan.deployment`
  which controls how operator manages `DataPlane` `Deployment`'s during and after
  a rollout. This can currently take 1 value:
  - `ScaleDownOnPromotionScaleUpOnRollout` which will scale down the `DataPlane`
  preview deployment to 0 replicas before a rollout is triggered via a spec change.
  [kong/kong-operator-archive#1000](https://github.com/kong/kong-operator-archive/pull/1000)
- Added admission webhook validation on of `DataPlane` spec updates when the
  Blue Green promotion is in progress.
  [kong/kong-operator-archive#1051](https://github.com/kong/kong-operator-archive/pull/1051)
- Added `gateway-operator.konghq.com/wait-for-owner` finalizer to all dependent
  resources owned by `DataPlane` to prevent them from being mistakenly deleted.
  [kong/kong-operator-archive#1052](https://github.com/kong/kong-operator-archive/pull/1052)

### Fixes

- Fixes setting `status.ready` and `status.conditions` on the `DataPlane` when
  it's waiting for an address to be assigned to its LoadBalancer Ingress Service.
  [kong/kong-operator-archive#942](https://github.com/kong/kong-operator-archive/pull/942)
- Correctly set the `observedGeneration` on `DataPlane` and `ControlPlane` status
  conditions.
  [kong/kong-operator-archive#944](https://github.com/kong/kong-operator-archive/pull/944)
- Added annotation `gateway-operator.konghq.com/last-applied-annotations` to
  resources (e.g, Ingress `Services`s) owned by `DataPlane`s to store last
   applied annotations to the owned resource. If an annotation is present in the
  `gateway-operator.konghq.com/last-applied-annotations` annotation of an
  ingress `Service` but not present in the current specification of ingress
  `Service` annotations of the owning `DataPlane`, the annotation will be removed
  in the ingress `Service`.
  [kong/kong-operator-archive#936](https://github.com/kong/kong-operator-archive/pull/936)
- Correctly set the `Ready` condition in `DataPlane` status field during Blue
  Green promotion. The `DataPlane` is considered ready whenever it has its
  Deployment's `AvailableReplicas` equal to desired number of replicas (as per
  `spec.replicas`) and its Service has an IP assigned if it's of type `LoadBalancer`.
  [kong/kong-operator-archive#986](https://github.com/kong/kong-operator-archive/pull/986)
- Properly handles missing CRD during controller startup. Now whenever a CRD
  is missing during startup a clean log entry will be printed to inform a user
  why the controller was disabled.
  Additionally a check for `discovery.ErrGroupDiscoveryFailed` was added during
  CRD lookup.
  [kong/kong-operator-archive#1059](https://github.com/kong/kong-operator-archive/pull/1059)

### Changes

- Default the leader election namespace to controller namespace (`POD_NAMESPACE` env)
  instead of hardcoded "kong-system"
  [kong/kong-operator-archive#927](https://github.com/kong/kong-operator-archive/pull/927)
- Renamed `DataPlane` proxy service name and label to ingress
  [kong/kong-operator-archive#971](https://github.com/kong/kong-operator-archive/pull/971)
- Removed `DataPlane` `status.ready` as it couldn't be used reliably to represent
  `DataPlane`'s status. Users should now use `status.conditions`'s `Ready` condition
  and compare its `observedGeneration` with `DataPlane` `metadata.generation`
  to get an accurate representation of `DataPlane`'s readiness.
  [kong/kong-operator-archive#989](https://github.com/kong/kong-operator-archive/pull/989)
- Disable `ControlPlane` and `Gateway` controllers by default.
  Users who want to enable those can use the command line flags:
  - `-enable-controller-controlplane` and
  - `-enable-controller-gateway`
  At this time, the Gateway API and `ControlPlane` resources that these
  flags are considered a feature preview, and are not supported. Use these
  only in non-production scenarios until these features are graduated to GA.
  [kong/kong-operator-archive#1026](https://github.com/kong/kong-operator-archive/pull/1026)
- Bump `ControlPlane` default version to `v2.11.1` and remove support for older versions.
  To satisfy this change, use `Programmed` condition instead of `Ready` in Gateway
  Listeners status conditions to make `ControlPlane` be able to attach routes
  to those listeners.
  This stems from the fact that KIC `v2.11` bumped support for Gateway API to `v0.7.1`.
  [kong/kong-operator-archive#1041](https://github.com/kong/kong-operator-archive/pull/1041)
- Bump Gateway API to v0.7.1.
  [kong/kong-operator-archive#1047](https://github.com/kong/kong-operator-archive/pull/1047)
- Operator doesn't change the `DataPlane` resource anymore by filling it with
  Kong Gateway environment variables. Instead this is now happening on the fly
  so the `DataPlane` resources applied by users stay as submitted.
  [kong/kong-operator-archive#1034](https://github.com/kong/kong-operator-archive/pull/1034)
- Don't use `Provisioned` status condition type on `DataPlane`s.
  From now on `DataPlane`s are only expressing their status through `Ready` status
  condtion.
  [kong/kong-operator-archive#1043](https://github.com/kong/kong-operator-archive/pull/1043)
- Bump default `DataPlane` image to 3.4
  [kong/kong-operator-archive#1067](https://github.com/kong/kong-operator-archive/pull/1067)
- When rollout strategy is removed from a `DataPlane` spec, preview subresources
  are removed.
  [kong/kong-operator-archive#1066](https://github.com/kong/kong-operator-archive/pull/1066)

## [v0.6.0]

> Release date: 2023-07-20

### Added

- Added `Ready`, `ReadyReplicas` and `Replicas` fields to `DataPlane`'s Status
  [kong/kong-operator-archive#854](https://github.com/kong/kong-operator-archive/pull/854)
- Added `Rollout` field to `DataPlane` CRD. This allows specification of rollout
  strategy and behavior (e.g. to enable blue/green rollouts for upgrades).
  [kong/kong-operator-archive#879](https://github.com/kong/kong-operator-archive/pull/879)
- Added `Rollout` status fields to `DataPlane` CRD.
  [kong/kong-operator-archive#896](https://github.com/kong/kong-operator-archive/pull/896)

### Changes

> **WARN**: Breaking changes included

- Renamed `Services` options in `DataPlaneOptions` to `Network` options, which
  now includes `IngressService` as one of the sub-attributes.
  This is a **breaking change** which requires some renaming and reworking of
  struct attribute access.
  [kong/kong-operator-archive#849](https://github.com/kong/kong-operator-archive/pull/849)
- Bump Gateway API to v0.6.2 and enable Gateway API conformance testing.
  [kong/kong-operator-archive#853](https://github.com/kong/kong-operator-archive/pull/853)
- Add `PodTemplateSpec` to `DeploymentOptions` to allow applying strategic merge
  patcher on top of `Pod`s generated by the operator.
  This is a **breaking change** which requires manual porting from `Pods` field
  to `PodTemplateSpec`.
  More info on strategic merge patch can be found in official Kubernetes docs at
  [sig-api-machinery/strategic-merge-patch.md][strategic-merge-patch].
  [kong/kong-operator-archive#862](https://github.com/kong/kong-operator-archive/pull/862)
- Added `v1beta1` version of the `DataPlane` API, which replaces the `v1alpha1`
  version. The `v1alpha1` version of the API has been removed entirely in favor
  of the new version to reduce maintenance costs.
  [kong/kong-operator-archive#905](https://github.com/kong/kong-operator-archive/pull/905)

[strategic-merge-patch]: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-api-machinery/strategic-merge-patch.md

### Fixes

- Fixes setting `Affinity` when generating `Deployment`s for `DataPlane`s
  `ControlPlane`s which caused 2 `ReplicaSet`s to be created where the first
  one should already have the `Affinity` set making the update unnecessary.
  [kong/kong-operator-archive#894](https://github.com/kong/kong-operator-archive/pull/894)

## [v0.5.0]

> Release date: 2023-06-20

### Added

- Added `AddressSourceType` to `DataPlane` status `Address`
  [kong/kong-operator-archive#798](https://github.com/kong/kong-operator-archive/pull/798)
- Add pod Affinity field to `PodOptions` and support for both `DataPlane` and `ControlPlane`
- Add Kong Gateway enterprise image - `kong/kong-gateway` - to the set of supported
  `DataPlane` images.
  [kong/kong-operator-archive#749](https://github.com/kong/kong-operator-archive/pull/749)
- Moved pod related options in `DeploymentOptions` to `PodsOptions` and added pod
  labels option.
  [kong/kong-operator-archive#742](https://github.com/kong/kong-operator-archive/pull/742)
- Added `Volumes` and `VolumeMounts` field in `DeploymentOptions` of `DataPlane`
  specs. Users can attach custom volumes and mount the volumes to proxy container
  of pods in `Deployments` of dataplanes.
  Note: `Volumes` and `VolumeMounts` are not supported for `ControlPlane` specs now.
  [kong/kong-operator-archive#681](https://github.com/kong/kong-operator-archive/pull/681)
- Added possibility to replicas on `DataPlane` deployments
  This allows users to define `DataPlane`s - without `ControlPlane` - to be
  horizontally scalable.
  [kong/kong-operator-archive#737](https://github.com/kong/kong-operator-archive/pull/737)
- Added possibility to specify `DataPlane` proxy service type
  [kong/kong-operator-archive#739](https://github.com/kong/kong-operator-archive/pull/739)
- Added possibility to specify resources through `DataPlane` and `ControlPlane`
  `spec.deployment.resources`
  [kong/kong-operator-archive#712](https://github.com/kong/kong-operator-archive/pull/712)
- The `DataPlane` spec has been updated with a new field related
  to the proxy service. By using such a field, it is possible to
  specify annotations to be set on the `DataPlane` proxy service.
  [kong/kong-operator-archive#682](https://github.com/kong/kong-operator-archive/pull/682)

### Changed

- Bumped default ControlPlane image to 2.9.3
  [kong/kong-operator-archive#712](https://github.com/kong/kong-operator-archive/pull/712)
  [kong/kong-operator-archive#719](https://github.com/kong/kong-operator-archive/pull/719)
- Bumped default DataPlane image to 3.2.2
  [kong/kong-operator-archive#728](https://github.com/kong/kong-operator-archive/pull/728)
- Bumped Gateway API to 0.6.1. Along with it, the deprecated `Gateway`
  `scheduled` condition has been replaced by the `accepted` condition.
  [kong/kong-operator-archive#618](https://github.com/kong/kong-operator-archive/issues/618)
- `ControlPlane` and `DataPlane` specs have been refactored by explicitly setting
  the deployment field (instead of having it inline).
  [kong/kong-operator-archive#725](https://github.com/kong/kong-operator-archive/pull/725)
- `ControlPlane` and `DataPlane` specs now require users to provide `containerImage`
  and `version` fields.
  This is being enforced in the admission webhook.
  [kong/kong-operator-archive#758](https://github.com/kong/kong-operator-archive/pull/758)
- Validation for `ControlPlane` and `DataPlane` components no longer has a
  "ceiling", or maximum version. This due to popular demand, but now puts more
  emphasis on the user to troubleshoot when things go wrong. It's no longer
  possible to use a tag that's not semver compatible (e.g. 2.10.0) for these
  components (for instance, a branch such as `main`) without enabling developer
  mode.
  [kong/kong-operator-archive#819](https://github.com/kong/kong-operator-archive/pull/819)
- `ControlPlane` and `DataPlane` image validation now supports enterprise image
  flavours, e.g. `3.3.0-ubuntu`, `3.2.0.0-rhel` etc.
  [kong/kong-operator-archive#830](https://github.com/kong/kong-operator-archive/pull/830)

### Fixes

- Fix admission webhook certificates Job which caused TLS handshake errors when
  webhook was being called.
  [kong/kong-operator-archive#716](https://github.com/kong/kong-operator-archive/pull/716)
- Include leader election related role when generating `ControlPlane` RBAC
  manifests so that Gateway Discovery can be used by KIC.
  [kong/kong-operator-archive#743](https://github.com/kong/kong-operator-archive/pull/743)

## [v0.4.0]

> Release date: 2022-01-25

### Added

- Added machinery for ControlPlanes to communicate with DataPlanes
  directly via Pod IPs. The Admin API has been removed from the LoadBalancer service.
  [kong/kong-operator-archive#609](https://github.com/kong/kong-operator-archive/pull/609)
- The Gateway Listeners status is set and kept up to date by the Gateway controller.
  [kong/kong-operator-archive#627](https://github.com/kong/kong-operator-archive/pull/627)

## [v0.3.0]

> Release date: 2022-11-30

> Maturity: ALPHA

### Changed

- Bumped DataPlane default image to 3.0.1
  [kong/kong-operator-archive#561](https://github.com/kong/kong-operator-archive/pull/561)

### Added

- Gateway statuses now include all addresses from their DataPlane Service.
  [kong/kong-operator-archive#535](https://github.com/kong/kong-operator-archive/pull/535)
- DataPlane Deployment strategy enforced as RollingUpdate.
  [kong/kong-operator-archive#537](https://github.com/kong/kong-operator-archive/pull/537)

### Fixes

- Regenerate DataPlane's TLS secret upon deletion
  [kong/kong-operator-archive#500](https://github.com/kong/kong-operator-archive/pull/500)
- Gateway statuses no longer list cluster IPs if their DataPlane Service is a
  LoadBalancer.
  [kong/kong-operator-archive#535](https://github.com/kong/kong-operator-archive/pull/535)

## [v0.2.0]

> Release date: 2022-10-26

> Maturity: ALPHA

### Added

- Updated default Kong version to 3.0.0
- Updated default Kubernetes Ingress Controller version to 2.7
- Update DataPlane and ControlPlane Ready condition when underlying Deployment
  changes Ready condition
  [kong/kong-operator-archive#451](https://github.com/kong/kong-operator-archive/pull/451)
- Update DataPlane NetworkPolicy to match KONG_PROXY_LISTEN and KONG_ADMIN_LISTEN
  environment variables set in DataPlane
  [kong/kong-operator-archive#473](https://github.com/kong/kong-operator-archive/pull/473)
- Added Container image and version validation for ControlPlanes and DataPlanes.
  The operator now only supports the Kubernetes-ingress-controller (2.7) as
  the ControlPlane, and Kong (3.0) as the DataPlane.
  [kong/kong-operator-archive#490](https://github.com/kong/kong-operator-archive/pull/490)
- DataPlane resources get a new `Status` field: `Addresses` which will contain
  backing service addresses.
  [kong/kong-operator-archive#483](https://github.com/kong/kong-operator-archive/pull/483)

## [v0.1.1]

> Release date:  2022-09-24

> Maturity: ALPHA

### Added

- `HTTPRoute` support was added. If version of control plane image is at
  least 2.6, the `Gateway=true` feature gate is enabled, so the
  control plane can pick up the `HTTPRoute` and configure it on data plane.
  [kong/kong-operator-archive#302](https://github.com/kong/kong-operator-archive/pull/302)

## [v0.1.0]

> Release date: 2022-09-15

> Maturity: ALPHA

This is the initial release which includes basic functionality at an alpha
level of maturity and includes some of the fundamental APIs needed to create
gateways for ingress traffic.

### Initial Features

- The `GatewayConfiguration` API was added to enable configuring `Gateway`
  resources with the options needed to influence the configuration of
  the underlying `ControlPlane` and `DataPlane` resources.
  [kong/kong-operator-archive#43](https://github.com/kong/kong-operator-archive/pull/43)
- `GatewayClass` support was added to delineate which `Gateway` resources the
  operator supports.
  [kong/kong-operator-archive#22](https://github.com/kong/kong-operator-archive/issues/22)
- `Gateway` support was added: used to create edge proxies for ingress traffic.
  [kong/kong-operator-archive#6](https://github.com/kong/kong-operator-archive/issues/6)
- The `ControlPlane` API was added to deploy Kong Ingress Controllers which
  can be attached to `DataPlane` resources.
  [kong/kong-operator-archive#5](https://github.com/kong/kong-operator-archive/issues/5)
- The `DataPlane` API was added to deploy Kong Gateways.
  [kong/kong-operator-archive#4](https://github.com/kong/kong-operator-archive/issues/4)
- The operator manages certificates for control and data plane communication
  and configures mutual TLS between them. It cannot yet replace expired
  certificates.
  [kong/kong-operator-archive#103](https://github.com/kong/kong-operator-archive/issues/103)

### Known issues

When deploying the gateway-operator through the bundle, there might be some
leftovers from previous operator deployments in the cluster. The user needs to delete all the cluster-wide leftovers
(clusterrole, clusterrolebinding, validatingWebhookConfiguration) before
re-installing the operator through the bundle.

[v2.1.0-alpha.0]: https://github.com/Kong/kong-operator/compare/v2.0.5..v2.1.0-alpha.0
[v2.0.5]: https://github.com/Kong/kong-operator/compare/v2.0.4..v2.0.5
[v2.0.4]: https://github.com/Kong/kong-operator/compare/v2.0.3..v2.0.4
[v2.0.3]: https://github.com/Kong/kong-operator/compare/v2.0.2..v2.0.3
[v2.0.2]: https://github.com/Kong/kong-operator/compare/v2.0.1..v2.0.2
[v2.0.1]: https://github.com/Kong/kong-operator/compare/v2.0.0..v2.0.1
[v2.0.0]: https://github.com/Kong/kong-operator/compare/v1.6.2..v2.0.0
[v1.6.2]: https://github.com/Kong/kong-operator/compare/v1.6.1..v1.6.2
[v1.6.1]: https://github.com/Kong/kong-operator/compare/v1.6.0..v1.6.1
[v1.6.0]: https://github.com/Kong/kong-operator/compare/v1.5.1..v1.6.0
[v1.5.1]: https://github.com/kong/kong-operator/compare/v1.5.0..v1.5.1
[v1.5.0]: https://github.com/kong/kong-operator/compare/v1.4.2..v1.5.0
[v1.4.2]: https://github.com/kong/kong-operator/compare/v1.4.1..v1.4.2
[v1.4.1]: https://github.com/kong/kong-operator/compare/v1.4.0..v1.4.1
[v1.4.0]: https://github.com/kong/kong-operator/compare/v1.3.0..v1.4.0
[v1.3.0]: https://github.com/kong/kong-operator/compare/v1.2.3..v1.3.0
[v1.2.3]: https://github.com/kong/kong-operator/compare/v1.2.2..v1.2.3
[v1.2.2]: https://github.com/kong/kong-operator/compare/v1.2.1..v1.2.2
[v1.2.1]: https://github.com/kong/kong-operator/compare/v1.2.0..v1.2.1
[v1.2.0]: https://github.com/kong/kong-operator/compare/v1.1.0..v1.2.0
[v1.1.0]: https://github.com/kong/kong-operator/compare/v1.0.3..v1.1.0
[v1.0.3]: https://github.com/kong/kong-operator/compare/v1.0.2..v1.0.3
[v1.0.2]: https://github.com/kong/kong-operator/compare/v1.0.1..v1.0.2
[v1.0.1]: https://github.com/kong/kong-operator/compare/v1.0.0..v1.0.1
[v1.0.0]: https://github.com/kong/kong-operator/compare/v0.7.0..v1.0.0
[v0.7.0]: https://github.com/kong/kong-operator/compare/v0.6.0..v0.7.0
[v0.6.0]: https://github.com/kong/kong-operator/compare/v0.5.0..v0.6.0
[v0.5.0]: https://github.com/kong/kong-operator/compare/v0.4.0..v0.5.0
[v0.4.0]: https://github.com/kong/kong-operator/compare/v0.3.0..v0.4.0
[v0.3.0]: https://github.com/kong/kong-operator/compare/v0.2.0..v0.3.0
[v0.2.0]: https://github.com/kong/kong-operator/compare/v0.1.0..v0.2.0
[v0.1.1]: https://github.com/kong/kong-operator/compare/v0.0.1..v0.1.1
[v0.1.0]: https://github.com/kong/kong-operator/compare/v0.0.0..v0.1.0
