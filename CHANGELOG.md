# Changelog

## Table of Contents

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

### Fixed

- Correctly assume default Kong router flavor is `traditional_compatible` when
  `KONG_ROUTER_FLAVOR` is not set. This fixes incorrectly populated
  `GatewayClass.status.supportedFeatures` when the default was assumed to be
  `expressions`.
  [#2043](https://github.com/Kong/kong-operator/pull/2043)
- Support setting exposed nodeport of the dataplane service for `Gateway`s by
  `nodePort` field in `spec.listenersOptions`.
  [#2058](https://github.com/Kong/kong-operator/pull/2058)

## [v1.6.2]

> Release date: 2025-07-11

### Fixed

- Ignore the `ForbiddenError` in `sdk-konnect-go` returned from running CRUD
  operations against Konnect APIs. This prevents endless reconciliation when an
  operation is not allowed (due to e.g. exhausted quota).
  [#1811](https://github.com/Kong/kong-operator/pull/1811)

## [v1.6.1]

> Release date: 2025-05-22

## Changed

- Allowed the `kubectl rollout restart` operation for Deployment resources created via DataPlane CRD.
  [#1660](https://github.com/Kong/gateway-operator/pull/1660)

## [v1.6.0]

> Release date: 2025-05-07

### Added

- In `KonnectGatewayControlPlane` fields `Status.Endpoints.ControlPlaneEndpoint`
  and `Status.Endpoints.TelemetryEndpoint` are filled with respective values from Konnect.
  [#1415](https://github.com/Kong/gateway-operator/pull/1415)
- Add `namespacedRef` support for referencing networks in `KonnectCloudGatewayDataPlaneGroupConfiguration`
  [#1423](https://github.com/Kong/gateway-operator/pull/1423)
- Introduced new CLI flags:
  - `--logging-mode` (or `GATEWAY_OPERATOR_LOGGING_MODE` env var) to set the logging mode (`development` can be set
    for simplified logging).
  - `--validate-images` (or `GATEWAY_OPERATOR_VALIDATE_IMAGES` env var) to enable ControlPlane and DataPlane image
    validation (it's set by default to `true`).
  [#1435](https://github.com/Kong/gateway-operator/pull/1435)
- Add support for `-enforce-config` for `ControlPlane`'s `ValidatingWebhookConfiguration`.
  This allows to use operator's `ControlPlane` resources in AKS clusters.
  [#1512](https://github.com/Kong/gateway-operator/pull/1512)
- `KongRoute` can be migrated from serviceless to service bound and vice versa.
  [#1492](https://github.com/Kong/gateway-operator/pull/1492)
- Add `KonnectCloudGatewayTransitGateway` controller to support managing Konnect
  transit gateways.
  [#1489](https://github.com/Kong/gateway-operator/pull/1489)
- Added support for setting `PodDisruptionBudget` in `GatewayConfiguration`'s `DataPlane` options.
  [#1526](https://github.com/Kong/gateway-operator/pull/1526)
- Added `spec.watchNamespace` field to `ControlPlane` and `GatewayConfiguration` CRDs
  to allow watching resources only in the specified namespace.
  When `spec.watchNamespace.type=list` is used, each specified namespace requires
  a `WatchNamespaceGrant` that allows the `ControlPlane` to watch resources in the specified namespace.
  Aforementioned list is extended with `ControlPlane`'s own namespace which doesn't
  require said `WatchNamespaceGrant`.
  [#1388](https://github.com/Kong/gateway-operator/pull/1388)
  [#1410](https://github.com/Kong/gateway-operator/pull/1410)
  [#1555](https://github.com/Kong/gateway-operator/pull/1555)
  <!-- TODO: https://github.com/Kong/gateway-operator/issues/1501 add link to guide from documentation. -->
  For more information on this please see: https://docs.konghq.com/gateway-operator/latest/
- Implemented `Mirror` and `Origin` `KonnectGatewayControlPlane`s.
  [#1496](https://github.com/Kong/gateway-operator/pull/1496)

### Changes

- Deduce `KonnectCloudGatewayDataPlaneGroupConfiguration` region based on the attached
  `KonnectAPIAuthConfiguration` instead of using a hardcoded `eu` value.
  [#1409](https://github.com/Kong/gateway-operator/pull/1409)
- Support `NodePort` as ingress service type for `DataPlane`
  [#1430](https://github.com/Kong/gateway-operator/pull/1430)
- Allow setting `NodePort` port number for ingress service for `DataPlane`.
  [#1516](https://github.com/Kong/gateway-operator/pull/1516)
- Updated `kubernetes-configuration` dependency for adding `scale` subresource for `DataPlane` CRD.
  [#1523](https://github.com/Kong/gateway-operator/pull/1523)
- Bump `kong/kubernetes-configuration` dependency to v1.4.0
  [#1574](https://github.com/Kong/gateway-operator/pull/1574)

### Fixes

- Fix setting the defaults for `GatewayConfiguration`'s `ReadinessProbe` when only
  timeouts and/or delays are specified. Now the HTTPGet field is set to `/status/ready`
  as expected with the `Gateway` scenario.
  [#1395](https://github.com/Kong/gateway-operator/pull/1395)
- Fix ingress service name not being applied when using `GatewayConfiguration`.
  [#1515](https://github.com/Kong/gateway-operator/pull/1515)
- Fix ingress service port name setting.
  [#1524](https://github.com/Kong/gateway-operator/pull/1524)

## [v1.5.1]

> Release date: 2025-04-01

### Added

- Add `namespacedRef` support for referencing networks in `KonnectCloudGatewayDataPlaneGroupConfiguration`
  [#1425](https://github.com/Kong/gateway-operator/pull/1425)
- Set `ControlPlaneRefValid` condition to false when reference to `KonnectGatewayControlPlane` is invalid
  [#1421](https://github.com/Kong/gateway-operator/pull/1421)

### Changes

- Deduce `KonnectCloudGatewayDataPlaneGroupConfiguration` region based on the attached
  `KonnectAPIAuthConfiguration` instead of using a hardcoded `eu` value.
  [#1417](https://github.com/Kong/gateway-operator/pull/1417)
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
  [#1161](https://github.com/Kong/gateway-operator/pull/1161)
- Migrate KGO CRDs to the kubernetes-configuration repo.
  With this migration process, we have removed the `api` and `pkg/clientset` from the KGO repo.
  This is a breaking change which requires manual action for projects that use operator's Go APIs.
  In order to migrate please use the import paths from the [kong/kubernetes-configuration](kubernetes-configuration) repo instead.
  For example:
  `github.com/kong/gateway-operator/api/v1beta1` becomes 
  `github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1`.
  [#1148](https://github.com/Kong/gateway-operator/pull/1148)
- Support for the `konnect-extension.gateway-operator.konghq.com` CRD has been interrupted. The new
  API `konnect-extension.konnect.konghq.com` must be used instead. The migration path is described in
  the [Kong documentation](https://docs.konghq.com/gateway-operator/latest/guides/migrating/migrate-from-1.4-to-1.5/).
  [#1183](https://github.com/Kong/gateway-operator/pull/1183)
- Migrate KGO CRDs conditions to the kubernetes-configuration repo.
  With this migration process, we have moved all conditions from the KGO repo to [kubernetes-configuration](kubernetes-configuration).
  This is a breaking change which requires manual action for projects that use operator's Go conditions types.
  In order to migrate please use the import paths from the [kong/kubernetes-configuration](kubernetes-configuration) repo instead.
  [#1281](https://github.com/Kong/gateway-operator/pull/1281)
  [#1305](https://github.com/Kong/gateway-operator/pull/1305)
  [#1306](https://github.com/Kong/gateway-operator/pull/1306)
  [#1318](https://github.com/Kong/gateway-operator/pull/1318)

[kubernetes-configuration]: https://github.com/Kong/kubernetes-configuration

### Added

- Added `Name` field in `ServiceOptions` to allow specifying name of the
  owning service. Currently specifying ingress service of `DataPlane` is
  supported.
  [#966](https://github.com/Kong/gateway-operator/pull/966)
- Added support for global plugins with `KongPluginBinding`'s `scope` field.
  The default value is `OnlyTargets` which means that the plugin will be
  applied only to the targets specified in the `targets` field. The new
  alternative is `GlobalInControlPlane` that will make the plugin apply
  globally in a control plane.
  [#1052](https://github.com/Kong/gateway-operator/pull/1052)
- Added `-cluster-ca-key-type` and `-cluster-ca-key-size` CLI flags to allow
  configuring cluster CA private key type and size. Currently allowed values:
  `rsa` and `ecdsa` (default).
  [#1081](https://github.com/Kong/gateway-operator/pull/1081)
- The `GatewayClass` Accepted Condition is set to `False` with reason `InvalidParameters`
  in case the `.spec.parametersRef` field is not a valid reference to an existing
  `GatewayConfiguration` object.
  [#1021](https://github.com/Kong/gateway-operator/pull/1021)
- The `SupportedFeatures` field is properly set in the `GatewayClass` status.
  It requires the experimental version of Gateway API (as of v1.2.x) installed in
  your cluster, and the flag `--enable-gateway-api-experimental` set.
  [#1010](https://github.com/Kong/gateway-operator/pull/1010)
- Added support for `KongConsumer` `credentials` in Konnect entities support.
  Users can now specify credentials for `KongConsumer`s in `Secret`s and reference
  them in `KongConsumer`s' `credentials` field.
  - `basic-auth` [#1120](https://github.com/Kong/gateway-operator/pull/1120)
  - `key-auth` [#1168](https://github.com/Kong/gateway-operator/pull/1168)
  - `acl` [#1187](https://github.com/Kong/gateway-operator/pull/1187)
  - `jwt` [#1208](https://github.com/Kong/gateway-operator/pull/1208)
  - `hmac` [#1222](https://github.com/Kong/gateway-operator/pull/1222)
- Added prometheus metrics for Konnect entity operations in the metrics server:
  - `gateway_operator_konnect_entity_operation_count` for number of operations.
  - `gateway_operator_konnect_entity_operation_duration_milliseconds` for duration of operations.
  [#953](https://github.com/Kong/gateway-operator/pull/953)
- Added support for `KonnectCloudGatewayNetwork` CRD which can manage Konnect
  Cloud Gateway Network entities.
  [#1136](https://github.com/Kong/gateway-operator/pull/1136)
- Reconcile affected `KonnectExtension`s when the `Secret` used as Dataplane
  certificate is modified. A secret must have the `konghq.com/konnect-dp-cert`
  label to trigger the reconciliation.
  [#1250](https://github.com/Kong/gateway-operator/pull/1250)
- When the `DataPlane` is configured in Konnect, the `/status/ready` endpoint
  is set as the readiness probe.
  [#1235](https://github.com/Kong/gateway-operator/pull/1253)
- Added support for `KonnectDataPlaneGroupConfiguration` CRD which can manage Konnect
  Cloud Gateway DataPlane Group configurations entities.
  [#1186](https://github.com/Kong/gateway-operator/pull/1186)
- Supported `KonnectExtension` to attach to Konnect control planes by setting
  namespace and name of `KonnectGatewayControlPlane` in `spec.konnectControlPlane`.
  [#1254](https://github.com/Kong/gateway-operator/pull/1254)
- Added support for `KonnectExtension`s on `ControlPlane`s.
  [#1262](https://github.com/Kong/gateway-operator/pull/1262)
- Added support for `KonnectExtension`'s `status` `controlPlaneRefs` and `dataPlaneRefs`
  fields.
  [#1297](https://github.com/Kong/gateway-operator/pull/1297)
- Added support for `KonnectExtension`s on `Gateway`s via `GatewayConfiguration`
  extensibility.
  [#1292](https://github.com/Kong/gateway-operator/pull/1292)
- Added `-enforce-config` flag to enforce the configuration of the `ControlPlane`
  and `DataPlane` `Deployment`s.
  [#1307](https://github.com/Kong/gateway-operator/pull/1307)
- Added Automatic secret provisioning for `KonnectExtension` certificates.
  [#1304](https://github.com/Kong/gateway-operator/pull/1304)

### Changed

- `KonnectExtension` does not require `spec.serverHostname` to be set by a user
  anymore - default is set to `konghq.com`.
  [#947](https://github.com/Kong/gateway-operator/pull/947)
- Support KIC 3.4
  [#972](https://github.com/Kong/gateway-operator/pull/972)
- Allow more than 1 replica for `ControlPlane`'s `Deployment` to support HA deployments of KIC.
  [#978](https://github.com/Kong/gateway-operator/pull/978)
- Removed support for the migration of legacy labels so upgrading the operator from 1.3 (or older) to 1.5.0,
  should be done through 1.4.1
  [#976](https://github.com/Kong/gateway-operator/pull/976)
- Move `ControlPlane` `image` validation to CRD CEL rules.
  [#984](https://github.com/Kong/gateway-operator/pull/984)
- Remove usage of `kube-rbac-proxy`.
  Its functionality of can be now achieved by using the new flag `--metrics-access-filter`
  (or a corresponding `GATEWAY_OPERATOR_METRICS_ACCESS_FILTER` env).
  The default value for the flag is `off` which doesn't restrict the access to the metrics
  endpoint. The flag can be set to `rbac` which will configure KGO to verify the token
  sent with the request.
  For more information on this migration please consult
  [kubernetes-sigs/kubebuilder#3907][kubebuilder_3907].
  [#956](https://github.com/Kong/gateway-operator/pull/956)
- Move `DataPlane` ports validation to `ValidationAdmissionPolicy` and `ValidationAdmissionPolicyBinding`.
  [#1007](https://github.com/Kong/gateway-operator/pull/1007)
- Move `DataPlane` db mode validation to CRD CEL validation expressions.
  With this change only the `KONG_DATABASE` environment variable directly set in
  the `podTemplateSpec` is validated. `EnvFrom` is not evaluated anymore for this validation.
  [#1049](https://github.com/Kong/gateway-operator/pull/1049)
- Move `DataPlane` promotion in progress validation to CRD CEL validation expressions.
  This is relevant for `DataPlane`s with BlueGreen rollouts enabled only.
  [#1054](https://github.com/Kong/gateway-operator/pull/1054)
- Move `DataPlane`'s rollout strategy validation of disallowed `AutomaticPromotion`
  to CRD CEL validation expressions.
  This is relevant for `DataPlane`s with BlueGreen rollouts enabled only.
  [#1056](https://github.com/Kong/gateway-operator/pull/1056)
- Move `DataPlane`'s rollout resource strategy validation of disallowed `DeleteOnPromotionRecreateOnRollout`
  to CRD CEL validation expressions.
  This is relevant for `DataPlane`s with BlueGreen rollouts enabled only.
  [#1065](https://github.com/Kong/gateway-operator/pull/1065)
- The `GatewayClass` Accepted Condition is set to `False` with reason `InvalidParameters`
  in case the `.spec.parametersRef` field is not a valid reference to an existing
  `GatewayConfiguration` object.
  [#1021](https://github.com/Kong/gateway-operator/pull/1021)
- Validating webhook is now disabled by default. At this point webhook doesn't
  perform any validations.
  These were all moved either to CRD CEL validation expressions or to the
  `ValidationAdmissionPolicy`.
  Flag remains in place to not cause a breaking change for users that rely on it.
  [#1066](https://github.com/Kong/gateway-operator/pull/1066)
- Remove `ValidatingAdmissionWebhook` from the operator.
  As of now, all the validations have been moved to CRD CEL validation expressions
  or to the `ValidationAdmissionPolicy`.
  All the flags that were configuring the webhook are now deprecated and do not
  have any effect.
  They will be removed in next major release.
  [#1100](https://github.com/Kong/gateway-operator/pull/1100)
- Konnect entities that are attached to a Konnect CP through a `ControlPlaneRef`
  do not get an owner relationship set to the `ControlPlane` anymore hence
  they are not deleted when the `ControlPlane` is deleted.
  [#1099](https://github.com/Kong/gateway-operator/pull/1099)
- Remove the owner relationship between `KongService` and `KongRoute`.
  [#1178](https://github.com/Kong/gateway-operator/pull/1178)
- Remove the owner relationship between `KongTarget` and `KongUpstream`.
  [#1279](https://github.com/Kong/gateway-operator/pull/1279)
- Remove the owner relationship between `KongCertificate` and `KongSNI`.
  [#1285](https://github.com/Kong/gateway-operator/pull/1285)
- Remove the owner relationship between `KongKey`s and `KongKeysSet`s and `KonnectGatewayControlPlane`s.
  [#1291](https://github.com/Kong/gateway-operator/pull/1291)
- Check whether an error from calling Konnect API is a validation error by
  HTTP status code in Konnect entity controller. If the HTTP status code is
  `400`, we consider the error as a validation error and do not try to requeue
  the Konnect entity.
  [#1226](https://github.com/Kong/gateway-operator/pull/1226)
- Credential resources used as Konnect entities that are attached to a `KongConsumer`
  resource do not get an owner relationship set to the `KongConsumer` anymore hence
  they are not deleted when the `KongConsumer` is deleted.
  [#1259](https://github.com/Kong/gateway-operator/pull/1259)

[kubebuilder_3907]: https://github.com/kubernetes-sigs/kubebuilder/discussions/3907

### Fixes

- Fix `DataPlane`s with `KonnectExtension` and `BlueGreen` settings. Both the Live
  and preview deployments are now customized with Konnect-related settings.
  [#910](https://github.com/Kong/gateway-operator/pull/910)
- Remove `RunAsUser` specification in jobs to create webhook certificates
  because Openshift does not specifying `RunAsUser` by default.
  [#964](https://github.com/Kong/gateway-operator/pull/964)
- Fix watch predicates for types shared between KGO and KIC.
  [#948](https://github.com/Kong/gateway-operator/pull/948)
- Fix unexpected error logs caused by passing an odd number of arguments to the logger
  in the `KongConsumer` reconciler.
  [#983](https://github.com/Kong/gateway-operator/pull/983)
- Fix checking status when using a `KonnectGatewayControlPlane` with KIC CP type
  as a `ControlPlaneRef`.
  [#1115](https://github.com/Kong/gateway-operator/pull/1115)
- Fix setting `DataPlane`'s readiness probe using `GatewayConfiguration`.
  [#1118](https://github.com/Kong/gateway-operator/pull/1118)
- Fix handling Konnect API conflicts.
  [#1176](https://github.com/Kong/gateway-operator/pull/1176)

## [v1.4.2]

> Release date: 2025-01-23

### Fixes

- Bump `kong/kubernetes-configuration` dependency to v1.0.8 that fixes the issue with `spec.headers`
  in `KongRoute` CRD by aligning to the expected schema (instead of `map[string]string`, it should be
  `map[string][]string`).
  Please make sure you update the KGO channel CRDs accordingly in your cluster:
  `kustomize build github.com/Kong/kubernetes-configuration/config/crd/gateway-operator\?ref=v1.0.8 | kubectl apply -f -`
  [#1072](https://github.com/Kong/gateway-operator/pull/1072)

## [v1.4.1]

> Release date: 2024-11-28

### Fixes

- Fix setting the `ServiceAccountName` for `DataPlane`'s `Deployment`.
  [#897](https://github.com/Kong/gateway-operator/pull/897)
- Fixed setting `ExternalTrafficPolicy` on `DataPlane`'s ingress `Service` when
  the requested value is empty.
  [#898](https://github.com/Kong/gateway-operator/pull/898)
- Set 0 members on `KonnectGatewayControlPlane` which type is set to group.
  [#896](https://github.com/Kong/gateway-operator/pull/896)
- Fixed a `panic` in `KonnectAPIAuthConfigurationReconciler` occuring when nil
  response was returned by Konnect API when fetching the organization information.
  [#901](https://github.com/Kong/gateway-operator/pull/901)
- Bump sdk-konnect-go version to 0.1.10 to fix handling global API endpoints.
  [#894](https://github.com/Kong/gateway-operator/pull/894)

## [v1.4.0]

> Release date: 2024-10-31

### Added

- Proper `User-Agent` header is now set on outgoing HTTP requests.
  [#387](https://github.com/Kong/gateway-operator/pull/387)
- Introduce `KongPluginInstallation` CRD to allow installing custom Kong
  plugins distributed as container images.
  [#400](https://github.com/Kong/gateway-operator/pull/400), [#424](https://github.com/Kong/gateway-operator/pull/424), [#474](https://github.com/Kong/gateway-operator/pull/474), [#560](https://github.com/Kong/gateway-operator/pull/560), [#615](https://github.com/Kong/gateway-operator/pull/615), [#476](https://github.com/Kong/gateway-operator/pull/476)
- Extended `DataPlane` API with a possibility to specify `PodDisruptionBudget` to be
  created for the `DataPlane` deployments via `spec.resources.podDisruptionBudget`.
  [#464](https://github.com/Kong/gateway-operator/pull/464)
- Add `KonnectAPIAuthConfiguration` reconciler.
  [#456](https://github.com/Kong/gateway-operator/pull/456)
- Add support for Konnect tokens in `Secrets` in `KonnectAPIAuthConfiguration`
  reconciler.
  [#459](https://github.com/Kong/gateway-operator/pull/459)
- Add `KonnectControlPlane` reconciler.
  [#462](https://github.com/Kong/gateway-operator/pull/462)
- Add `KongService` reconciler for Konnect control planes.
  [#470](https://github.com/Kong/gateway-operator/pull/470)
- Add `KongUpstream` reconciler for Konnect control planes.
  [#593](https://github.com/Kong/gateway-operator/pull/593)
- Add `KongConsumer` reconciler for Konnect control planes.
  [#493](https://github.com/Kong/gateway-operator/pull/493)
- Add `KongRoute` reconciler for Konnect control planes.
  [#506](https://github.com/Kong/gateway-operator/pull/506)
- Add `KongConsumerGroup` reconciler for Konnect control planes.
  [#510](https://github.com/Kong/gateway-operator/pull/510)
- Add `KongCACertificate` reconciler for Konnect CA certificates.
  [#626](https://github.com/Kong/gateway-operator/pull/626)
- Add `KongCertificate` reconciler for Konnect Certificates.
  [#643](https://github.com/Kong/gateway-operator/pull/643)
- Added command line flags to configure the certificate generator job's images.
  [#516](https://github.com/Kong/gateway-operator/pull/516)
- Add `KongPluginBinding` reconciler for Konnect Plugins.
  [#513](https://github.com/Kong/gateway-operator/pull/513), [#535](https://github.com/Kong/gateway-operator/pull/535)
- Add `KongTarget` reconciler for Konnect Targets.
  [#627](https://github.com/Kong/gateway-operator/pull/627)
- Add `KongVault` reconciler for Konnect Vaults.
  [#597](https://github.com/Kong/gateway-operator/pull/597)
- Add `KongKey` reconciler for Konnect Keys.
  [#646](https://github.com/Kong/gateway-operator/pull/646)
- Add `KongKeySet` reconciler for Konnect KeySets.
  [#657](https://github.com/Kong/gateway-operator/pull/657)
- Add `KongDataPlaneClientCertificate` reconciler for Konnect DataPlaneClientCertificates.
  [#694](https://github.com/Kong/gateway-operator/pull/694)
- The `KonnectExtension` CRD has been introduced. Such a CRD can be attached
  to a `DataPlane` via the extensions field to have a konnect-flavored `DataPlane`.
  [#453](https://github.com/Kong/gateway-operator/pull/453),
  [#578](https://github.com/Kong/gateway-operator/pull/578),
  [#736](https://github.com/Kong/gateway-operator/pull/736)
- Entities created in Konnect are now labeled (or tagged for those that does not
  support labels) with origin Kubernetes object's metadata: `k8s-name`, `k8s-namespace`,
  `k8s-uid`, `k8s-generation`, `k8s-kind`, `k8s-group`, `k8s-version`.
  [#565](https://github.com/Kong/gateway-operator/pull/565)
- Add `KongService`, `KongRoute`, `KongConsumer`, and `KongConsumerGroup` watchers
  in the `KongPluginBinding` reconciler.
  [#571](https://github.com/Kong/gateway-operator/pull/571)
- Annotating the following resource with the `konghq.com/plugins` annotation results in
  the creation of a managed `KongPluginBinding` resource:
  - `KongService` [#550](https://github.com/Kong/gateway-operator/pull/550)
  - `KongRoute` [#644](https://github.com/Kong/gateway-operator/pull/644)
  - `KongConsumer` [#676](https://github.com/Kong/gateway-operator/pull/676)
  - `KongConsumerGroup` [#684](https://github.com/Kong/gateway-operator/pull/684)
  These `KongPluginBinding`s are taken by the `KongPluginBinding` reconciler
  to create the corresponding plugin objects in Konnect.
- `KongConsumer` associated with `ConsumerGroups` is now reconciled in Konnect by removing/adding
  the consumer from/to the consumer groups.
  [#592](https://github.com/Kong/gateway-operator/pull/592)
- Add support for `KongConsumer` credentials:
  - basic-auth [#625](https://github.com/Kong/gateway-operator/pull/625)
  - API key [#635](https://github.com/Kong/gateway-operator/pull/635)
  - ACL [#661](https://github.com/Kong/gateway-operator/pull/661)
  - JWT [#678](https://github.com/Kong/gateway-operator/pull/678)
  - HMAC Auth [#687](https://github.com/Kong/gateway-operator/pull/687)
- Add support for `KongRoute`s bound directly to `KonnectGatewayControlPlane`s (serviceless rotues).
  [#669](https://github.com/Kong/gateway-operator/pull/669)
- Allow setting `KonnectGatewayControlPlane`s group membership
  [#697](https://github.com/Kong/gateway-operator/pull/697)
- Apply Konnect-related customizations to `DataPlane`s that properly reference `KonnectExtension`
  resources.
  [#714](https://github.com/Kong/gateway-operator/pull/714)
- The KonnectExtension functionality is enabled only when the `--enable-controller-konnect`
  flag or the `GATEWAY_OPERATOR_ENABLE_CONTROLLER_KONNECT` env var is set.
  [#738](https://github.com/Kong/gateway-operator/pull/738)

### Fixed

- Fixed `ControlPlane` cluster wide resources not migrating to new ownership labels
  (introduced in 1.3.0) when upgrading the operator from 1.2 (or older) to 1.3.0.
  [#369](https://github.com/Kong/gateway-operator/pull/369)
- Requeue instead of reporting an error when a finalizer removal yields a conflict.
  [#454](https://github.com/Kong/gateway-operator/pull/454)
- Requeue instead of reporting an error when a GatewayClass status update yields a conflict.
  [#612](https://github.com/Kong/gateway-operator/pull/612)
- Guard object counters with checks whether CRDs for them exist
  [#710](https://github.com/Kong/gateway-operator/pull/710)
- Do not reconcile Gateways nor assign any finalizers when the referred GatewayClass is not supported.
  [#711](https://github.com/Kong/gateway-operator/pull/711)
- Fixed setting `ExternalTrafficPolicy` on `DataPlane`'s ingress `Service` during update and patch operations.
  [#750](https://github.com/Kong/gateway-operator/pull/750)
- Fixed setting `ExternalTrafficPolicy` on `DataPlane`'s ingress `Service`.
  Remove the default value (`Cluster`). Prevent setting this field for `ClusterIP` `Service`s.
  [#812](https://github.com/Kong/gateway-operator/pull/812)

### Changes

- Default version of `ControlPlane` is bumped to 3.3.1
  [#580](https://github.com/Kong/gateway-operator/pull/580)
- Default version of `DataPlane` is bumped to 3.8.0
  [#572](https://github.com/Kong/gateway-operator/pull/572)
- Gateway API has been bumped to v1.2.0
  [#674](https://github.com/Kong/gateway-operator/pull/674)

## [v1.3.0]

> Release date: 2024-06-24

### Added

- Add `ExternalTrafficPolicy` to `DataPlane`'s `ServiceOptions`
  [#241](https://github.com/Kong/gateway-operator/pull/241)

### Breaking Changes

- Changes project layout to match `kubebuilder` `v4`. Some import paths (due to dir renames) have changed
  `apis` -> `api` and `controllers` -> `controller`.
  [#84](https://github.com/Kong/gateway-operator/pull/84)

### Changes

- `Gateway` do not have their `Ready` status condition set anymore.
  This aligns with Gateway API and its conformance test suite.
  [#246](https://github.com/Kong/gateway-operator/pull/246)
- `Gateway`s' listeners now have their `attachedRoutes` count filled in in status.
  [#251](https://github.com/Kong/gateway-operator/pull/251)
- Detect when `ControlPlane` has its admission webhook disabled via
  `CONTROLLER_ADMISSION_WEBHOOK_LISTEN` environment variable and ensure that
  relevant webhook resources are not created/deleted.
  [#326](https://github.com/Kong/gateway-operator/pull/326)
- The `OwnerReferences` on cluster-wide resources to indicate their owner are now
  replaced by a proper set of labels to identify `kind`, `namespace`, and
  `name` of the owning object.
  [#259](https://github.com/Kong/gateway-operator/pull/259)
- Default version of `ControlPlane` is bumped to 3.2.0
  [#327](https://github.com/Kong/gateway-operator/pull/327)

### Fixes

- Fix enforcing up to date `ControlPlane`'s `ValidatingWebhookConfiguration`
  [#225](https://github.com/Kong/gateway-operator/pull/225)

## [v1.2.3]

### Fixes

> Release date: 2024-04-23

- Fixes an issue where managed `Gateway`s controller wasn't able to reduce
  the created `DataPlane` objects when too many have been created.
  [#43](https://github.com/Kong/gateway-operator/pull/43)
- `Gateway` controller will no longer set `DataPlane` deployment's replicas
  to default value when `DataPlaneOptions` in `GatewayConfiguration` define
  scaling strategy. This effectively allows users to use `DataPlane` horizontal
  autoscaling with `GatewayConfiguration` as the generated `DataPlane` deployment
  will no longer be rejected.
  [#79](https://github.com/Kong/gateway-operator/pull/79)
- Make creating a `DataPlane` index conditional based on enabling the `ControlPlane`
  controller. This allows running KGO without `ControlPlane` CRD with its controller
  disabled.
  [#103](https://github.com/Kong/gateway-operator/pull/103)

## [v1.2.2]

> Release date: 2024-04-23

### **NOTE: Retracted**

[v1.2.2][rel_122] was retracted due to a misplaced git tag.
Due to [golang proxy caching modules indefinitely][goproxy] we needed to retract this version.
[v1.2.3][rel_123] contains all the changes that v1.2.2 intended to contain.

[goproxy]: https://sum.golang.org/#faq-retract-version
[rel_122]: https://github.com/Kong/gateway-operator/releases/tag/v1.2.2
[rel_123]: https://github.com/Kong/gateway-operator/releases/tag/v1.2.3

## [v1.2.1]

> Release date: 2024-03-19

### Fixes

- Fixed an issue where operator wasn't able to update `ControlPlane` `ClusterRole` or `ClusterRoleBinding`
  when they got out of date.
  [#11](https://github.com/Kong/gateway-operator/pull/11)

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
  [Kong/gateway-operator-archive#1616](https://github.com/Kong/gateway-operator-archive/pull/1616)
- Add horizontal autoscaling for `DataPlane`s using its `scaling.horizontal` spec
  field.
  [Kong/gateway-operator-archive#1281](https://github.com/Kong/gateway-operator-archive/pull/1281)
- `ControlPlane`s now use Gateway Discovery by default, with Service DNS Strategy.
  Additionally, the `DataPlane` readiness probe has been changed to `/status/ready`
  when the `DataPlane` is managed by a `Gateway`.
  [Kong/gateway-operator-archive#1261](https://github.com/Kong/gateway-operator-archive/pull/1261)
- `Gateway`s and `Listener`s `Accepted` and `Conflicted` conditions are now set
  and enforced based on the Gateway API specifications.
  [Kong/gateway-operator-archive#1398](https://github.com/Kong/gateway-operator-archive/pull/1398)
- `ControlPlane` `ClusterRole`s and `ClusterRoleBinding`s are enforced and kept
  up to date by the `ControlPlane` controller.
  [Kong/gateway-operator-archive#1259](https://github.com/Kong/gateway-operator-archive/pull/1259)
- The `Gateway` listeners are now dynamically mapped to `DataPlane` ingress service
  ports. This means that the change of a `Gateway` spec leads to a `DataPlane` reconfiguration,
  along with an ingress service update.
  [Kong/gateway-operator-archive#1363](https://github.com/Kong/gateway-operator-archive/pull/1363)
- `--enable-controller-gateway` and `--enable-controller-controlplane` command
  line flags are set to `true` by default to enable controllers for `Gateway`s
  and `ControlPlane`s.
  [Kong/gateway-operator-archive#1519](https://github.com/Kong/gateway-operator-archive/pull/1519)
- When the `Gateway` controller provisions a `ControlPlane`, it sets the `CONTROLLER_GATEWAY_TO_RECONCILE`
  env variable to let the `ControlPlane` reconcile
  that specific `Gateway` only.
  [Kong/gateway-operator-archive#1529](https://github.com/Kong/gateway-operator-archive/pull/1529)
- `ControlPlane` is now deployed with a validating webhook server turned on. This
  involves creating `ValidatingWebhookConfiguration`, a `Service` that exposes the
  webhook and a `Secret` that holds a TLS certificate. The `Secret` is mounted in
  the `ControlPlane`'s `Pod` for the webhook server to use it.
  [Kong/gateway-operator-archive#1539](https://github.com/Kong/gateway-operator-archive/pull/1539)
  [Kong/gateway-operator-archive#1545](https://github.com/Kong/gateway-operator-archive/pull/1545)
- Added `konnectCertificate` field to the DataPlane resource.
  [Kong/gateway-operator-archive#1517](https://github.com/Kong/gateway-operator-archive/pull/1517)
- Added `v1alpha1.AIGateway` as an experimental API. This can be enabled by
  manually deploying the `AIGateway` CRD and enabling the feature on the
  controller manager with the `--enable-controller-aigateway` flag.
  [Kong/gateway-operator-archive#1399](https://github.com/Kong/gateway-operator-archive/pull/1399)
  [Kong/gateway-operator-archive#1542](https://github.com/Kong/gateway-operator-archive/pull/1399)
- Added validation on checking if ports in `KONG_PORT_MAPS` and `KONG_PROXY_LISTEN`
  environment variables of deployment options in `DataPlane` match the `ports`
  in the ingress service options of the `DataPlane`.
  [Kong/gateway-operator-archive#1521](https://github.com/Kong/gateway-operator-archive/pull/1521)

### Changes

- The `GatewayConfiguration` API has been promoted from `v1alpha1` to `v1beta1`.
  [Kong/gateway-operator-archive#1514](https://github.com/Kong/gateway-operator-archive/pull/1514)
- The `ControlPlane` API has been promoted from `v1alpha1` to `v1beta1`.
  [Kong/gateway-operator-archive#1523](https://github.com/Kong/gateway-operator-archive/pull/1523)
- The CRD's shortname of `ControlPlane` has been changed to `kocp`.
  The CRD's shortname of `DataPlane` has been changed to `kodp`.
  The CRD's shortname of `GatewayConfiguration` has been changed to `kogc`.
  [Kong/gateway-operator-archive#1532](https://github.com/Kong/gateway-operator-archive/pull/1532)
- `ControlPlane` (Kong Ingress Controller) default and minimum version has been
  bumped to 3.1.2.
  [Kong/gateway-operator-archive#1586](https://github.com/Kong/gateway-operator-archive/pull/1586)
- `DataPlane` (Kong Gateway) default version has been bumped to `v3.6.0`.
  [Kong/gateway-operator-archive#1577](https://github.com/Kong/gateway-operator-archive/pull/1577)

### Fixes

- Fixed a problem where the operator would not set the defaults to `PodTemplateSpec`
  patch and because of that it would detect a change and try to reconcile the owned
  resource where in fact the change was not there.
  One of the symptoms of this bug could have been a `StartupProbe` set in `PodSpec`
  preventing the `DataPlane` from getting correct status information.
  [Kong/gateway-operator-archive#1224](https://github.com/Kong/gateway-operator-archive/pull/1224)
- If the Gateway controller is enabled, `DataPlane` and `ControlPlane` controllers
  get enabled as well.
  [Kong/gateway-operator-archive#1242](https://github.com/Kong/gateway-operator-archive/pull/1242)
- Fix applying the `PodTemplateSpec` patch so that it's not applied when the
  calculated patch (resulting from the generated manifest and current in-cluster
  state) is empty.
  One of the symptoms of this bug was that when users tried to apply a `ReadinessProbe`
  which specified a port name instead of a number (which is what's generated by
  the operator) it would never reconcile and the status conditions would never get
  up to date `ObservedGeneration`.
  [Kong/gateway-operator-archive#1238](https://github.com/Kong/gateway-operator-archive/pull/1238)
- Fix manager RBAC permissions which prevented the operator from being able to
  create `ControlPlane`'s `ClusterRole`s, list pods or list `EndpointSlices`.
  [Kong/gateway-operator-archive#1255](https://github.com/Kong/gateway-operator-archive/pull/1255)
- `DataPlane`s with BlueGreen rollout strategy enabled will now have its Ready status
  condition updated to reflect "live" `Deployment` and `Service`s status.
  [Kong/gateway-operator-archive#1308](https://github.com/Kong/gateway-operator-archive/pull/1308)
- The `ControlPlane` `election-id` has been changed so that every `ControlPlane`
  has its own `election-id`, based on the `ControlPlane` name. This prevents `pod`s
  belonging to different `ControlPlane`s from competing for the same lease.
  [Kong/gateway-operator-archive#1349](https://github.com/Kong/gateway-operator-archive/pull/1349)
- Fill in the defaults for `env` and `volumes` when comparing the in-cluster spec
  with the generated spec.
  [Kong/gateway-operator-archive#1446](https://github.com/Kong/gateway-operator-archive/pull/1446)
- Do not flap `DataPlane`'s `Ready` status condition when e.g. ingress `Service`
  can't get an address assigned and `spec.network.services.ingress.`annotations`
  is non-empty.
  [Kong/gateway-operator-archive#1447](https://github.com/Kong/gateway-operator-archive/pull/1447)
- Update or recreate a `ClusterRoleBinding` for control planes if the existing
  one does not contain the `ServiceAccount` used by `ControlPlane`, or
  `ClusterRole` is changed.
  [Kong/gateway-operator-archive#1501](https://github.com/Kong/gateway-operator-archive/pull/1501)
- Retry reconciling `Gateway`s when provisioning owned `DataPlane` fails.
  [Kong/gateway-operator-archive#1553](https://github.com/Kong/gateway-operator-archive/pull/1553)

## [v1.1.0]

> Release date: 2023-11-20

### Added

- Add support for `ControlPlane` `v3.0` by updating the generated `ClusterRole`.
  [Kong/gateway-operator-archive#1189](https://github.com/Kong/gateway-operator-archive/pull/1189)

### Changes

- Bump `ControlPlane` default version to `v3.0`.
  [Kong/gateway-operator-archive#1189](https://github.com/Kong/gateway-operator-archive/pull/1189)
- Bump Gateway API to v1.0.
  [Kong/gateway-operator-archive#1189](https://github.com/Kong/gateway-operator-archive/pull/1189)

### Fixes

- Operator `Role` generation is fixed. As a result it contains now less rules
  hence the operator needs less permissions to run.
  [Kong/gateway-operator-archive#1191](https://github.com/Kong/gateway-operator-archive/pull/1191)

## [v1.0.3]

> Release date: 2023-11-06

### Fixes

- Fix an issue where operator is upgraded from an older version and it orphans
  old `DataPlane` resources.
  [Kong/gateway-operator-archive#1155](https://github.com/Kong/gateway-operator-archive/pull/1155)
  [Kong/gateway-operator-archive#1161](https://github.com/Kong/gateway-operator-archive/pull/1161)

### Added

- Setting `spec.deployment.podTemplateSpec.spec.volumes` and
  `spec.deployment.podTemplateSpec.spec.containers[*].volumeMounts` on `ControlPlane`s
  is now allowed.
  [Kong/gateway-operator-archive#1175](https://github.com/Kong/gateway-operator-archive/pull/1175)

## [v1.0.2]

> Release date: 2023-10-18

### Fixes

- Bump dependencies

## [v1.0.1]

> Release date: 2023-10-02

### Fixes

- Fix flapping of `Gateway` managed `ControlPlane` `spec` field when applied without
  `controlPlaneOptions` set.
  [Kong/gateway-operator-archive#1127](https://github.com/Kong/gateway-operator-archive/pull/1127)

### Changes

- Bump `ControlPlane` default version to `v2.12`.
  [Kong/gateway-operator-archive#1118](https://github.com/Kong/gateway-operator-archive/pull/1118)
- Bump `WebhookCertificateConfigBaseImage` to `v1.3.0`.
  [Kong/gateway-operator-archive#1130](https://github.com/Kong/gateway-operator-archive/pull/1130)

## [v1.0.0]

> Release date: 2023-09-26

### Changes

- Operator managed subresources are now labelled with `gateway-operator.konghq.com/managed-by`
  additionally to the old `konghq.com/gateway-operator` label.
  The value associated with this label stays the same and it still indicates the
  type of a resource that owns the subresrouce.
  The old label should not be used as it will be deleted in the future.
  [Kong/gateway-operator-archive#1098](https://github.com/Kong/gateway-operator-archive/pull/1098)
- Enable `DataPlane` Blue Green rollouts controller by default.
  [Kong/gateway-operator-archive#1106](https://github.com/Kong/gateway-operator-archive/pull/1106)

### Fixes

- Fixes handling `Volume`s and `VolumeMount`s when customizing through `DataPlane`'s
  `spec.deployment.podTemplateSpec.spec.containers[*].volumeMounts` and/or
  `spec.deployment.podTemplateSpec.spec.volumes`.
  Sample manifests are updated accordingly.
  [Kong/gateway-operator-archive#1095](https://github.com/Kong/gateway-operator-archive/pull/1095)

## [v0.7.0]

> Release date: 2023-09-13

### Added

- Added `gateway-operator.konghq.com/service-selector-override` as the dataplane
  annotation to override the default `Selector` of both the admin and proxy services.
  [Kong/gateway-operator-archive#921](https://github.com/Kong/gateway-operator-archive/pull/921)
- Added deploying of preview Admin API service when Blue Green rollout strategy
  is enabled for `DataPlane`s.
  `DataPlane`'s `status.rollout.service` is updated accordingly.
  [Kong/gateway-operator-archive#931](https://github.com/Kong/gateway-operator-archive/pull/931)
- Added `gateway-operator.konghq.com/promote-when-ready` `DataPlane` annotation to allow
  users to signal the operator should proceed with promoting the new resources when
  `BreakBeforePromotion` promotion strategy is used.
  [Kong/gateway-operator-archive#938](https://github.com/Kong/gateway-operator-archive/pull/938)
- Added deploying of preview Deployment when Blue Green rollout strategy
  is enabled for `DataPlane`s.
  [Kong/gateway-operator-archive#930](https://github.com/Kong/gateway-operator-archive/pull/930)
- Added appropriate label selectors to `DataPlane`s with enabled Blue Green rollout
  strategy. Now Admin Service and `DataPlane` Deployments correctly select their
  Pods.
  Added `DataPlane`'s `status.selector` and `status.rollout.deployment.selector` fields.
  [Kong/gateway-operator-archive#951](https://github.com/Kong/gateway-operator-archive/pull/951)
- Added setting rollout status with `RolledOut` condition
  [Kong/gateway-operator-archive#960](https://github.com/Kong/gateway-operator-archive/pull/960)
- Added deploying of preview ingress service for Blue Green rollout strategy.
  [Kong/gateway-operator-archive#956](https://github.com/Kong/gateway-operator-archive/pull/956)
- Implemented an actual promotion of a preview deployment to live state when BlueGreen
  rollout strategy is used.
  [Kong/gateway-operator-archive#966](https://github.com/Kong/gateway-operator-archive/pull/966)
- Added `PromotionFailed` condition which is set on `DataPlane`s with Blue Green
  rollout strategy when promotion related activities (like updating `DataPlane`
  service selector) fail.
  [Kong/gateway-operator-archive#1005](https://github.com/Kong/gateway-operator-archive/pull/1005)
- Added `spec.deployment.rollout.strategy.blueGreen.resources.plan.deployment`
  which controls how operator manages `DataPlane` `Deployment`'s during and after
  a rollout. This can currently take 1 value:
  - `ScaleDownOnPromotionScaleUpOnRollout` which will scale down the `DataPlane`
  preview deployment to 0 replicas before a rollout is triggered via a spec change.
  [Kong/gateway-operator-archive#1000](https://github.com/Kong/gateway-operator-archive/pull/1000)
- Added admission webhook validation on of `DataPlane` spec updates when the
  Blue Green promotion is in progress.
  [Kong/gateway-operator-archive#1051](https://github.com/Kong/gateway-operator-archive/pull/1051)
- Added `gateway-operator.konghq.com/wait-for-owner` finalizer to all dependent
  resources owned by `DataPlane` to prevent them from being mistakenly deleted.
  [Kong/gateway-operator-archive#1052](https://github.com/Kong/gateway-operator-archive/pull/1052)

### Fixes

- Fixes setting `status.ready` and `status.conditions` on the `DataPlane` when
  it's waiting for an address to be assigned to its LoadBalancer Ingress Service.
  [Kong/gateway-operator-archive#942](https://github.com/Kong/gateway-operator-archive/pull/942)
- Correctly set the `observedGeneration` on `DataPlane` and `ControlPlane` status
  conditions.
  [Kong/gateway-operator-archive#944](https://github.com/Kong/gateway-operator-archive/pull/944)
- Added annotation `gateway-operator.konghq.com/last-applied-annotations` to
  resources (e.g, Ingress `Services`s) owned by `DataPlane`s to store last
   applied annotations to the owned resource. If an annotation is present in the
  `gateway-operator.konghq.com/last-applied-annotations` annotation of an
  ingress `Service` but not present in the current specification of ingress
  `Service` annotations of the owning `DataPlane`, the annotation will be removed
  in the ingress `Service`.
  [Kong/gateway-operator-archive#936](https://github.com/Kong/gateway-operator-archive/pull/936)
- Correctly set the `Ready` condition in `DataPlane` status field during Blue
  Green promotion. The `DataPlane` is considered ready whenever it has its
  Deployment's `AvailableReplicas` equal to desired number of replicas (as per
  `spec.replicas`) and its Service has an IP assigned if it's of type `LoadBalancer`.
  [Kong/gateway-operator-archive#986](https://github.com/Kong/gateway-operator-archive/pull/986)
- Properly handles missing CRD during controller startup. Now whenever a CRD
  is missing during startup a clean log entry will be printed to inform a user
  why the controller was disabled.
  Additionally a check for `discovery.ErrGroupDiscoveryFailed` was added during
  CRD lookup.
  [Kong/gateway-operator-archive#1059](https://github.com/Kong/gateway-operator-archive/pull/1059)

### Changes

- Default the leader election namespace to controller namespace (`POD_NAMESPACE` env)
  instead of hardcoded "kong-system"
  [Kong/gateway-operator-archive#927](https://github.com/Kong/gateway-operator-archive/pull/927)
- Renamed `DataPlane` proxy service name and label to ingress
  [Kong/gateway-operator-archive#971](https://github.com/Kong/gateway-operator-archive/pull/971)
- Removed `DataPlane` `status.ready` as it couldn't be used reliably to represent
  `DataPlane`'s status. Users should now use `status.conditions`'s `Ready` condition
  and compare its `observedGeneration` with `DataPlane` `metadata.generation`
  to get an accurate representation of `DataPlane`'s readiness.
  [Kong/gateway-operator-archive#989](https://github.com/Kong/gateway-operator-archive/pull/989)
- Disable `ControlPlane` and `Gateway` controllers by default.
  Users who want to enable those can use the command line flags:
  - `-enable-controller-controlplane` and
  - `-enable-controller-gateway`
  At this time, the Gateway API and `ControlPlane` resources that these
  flags are considered a feature preview, and are not supported. Use these
  only in non-production scenarios until these features are graduated to GA.
  [Kong/gateway-operator-archive#1026](https://github.com/Kong/gateway-operator-archive/pull/1026)
- Bump `ControlPlane` default version to `v2.11.1` and remove support for older versions.
  To satisfy this change, use `Programmed` condition instead of `Ready` in Gateway
  Listeners status conditions to make `ControlPlane` be able to attach routes
  to those listeners.
  This stems from the fact that KIC `v2.11` bumped support for Gateway API to `v0.7.1`.
  [Kong/gateway-operator-archive#1041](https://github.com/Kong/gateway-operator-archive/pull/1041)
- Bump Gateway API to v0.7.1.
  [Kong/gateway-operator-archive#1047](https://github.com/Kong/gateway-operator-archive/pull/1047)
- Operator doesn't change the `DataPlane` resource anymore by filling it with
  Kong Gateway environment variables. Instead this is now happening on the fly
  so the `DataPlane` resources applied by users stay as submitted.
  [Kong/gateway-operator-archive#1034](https://github.com/Kong/gateway-operator-archive/pull/1034)
- Don't use `Provisioned` status condition type on `DataPlane`s.
  From now on `DataPlane`s are only expressing their status through `Ready` status
  condtion.
  [Kong/gateway-operator-archive#1043](https://github.com/Kong/gateway-operator-archive/pull/1043)
- Bump default `DataPlane` image to 3.4
  [Kong/gateway-operator-archive#1067](https://github.com/Kong/gateway-operator-archive/pull/1067)
- When rollout strategy is removed from a `DataPlane` spec, preview subresources
  are removed.
  [Kong/gateway-operator-archive#1066](https://github.com/Kong/gateway-operator-archive/pull/1066)

## [v0.6.0]

> Release date: 2023-07-20

### Added

- Added `Ready`, `ReadyReplicas` and `Replicas` fields to `DataPlane`'s Status
  [Kong/gateway-operator-archive#854](https://github.com/Kong/gateway-operator-archive/pull/854)
- Added `Rollout` field to `DataPlane` CRD. This allows specification of rollout
  strategy and behavior (e.g. to enable blue/green rollouts for upgrades).
  [Kong/gateway-operator-archive#879](https://github.com/Kong/gateway-operator-archive/pull/879)
- Added `Rollout` status fields to `DataPlane` CRD.
  [Kong/gateway-operator-archive#896](https://github.com/Kong/gateway-operator-archive/pull/896)

### Changes

> **WARN**: Breaking changes included

- Renamed `Services` options in `DataPlaneOptions` to `Network` options, which
  now includes `IngressService` as one of the sub-attributes.
  This is a **breaking change** which requires some renaming and reworking of
  struct attribute access.
  [Kong/gateway-operator-archive#849](https://github.com/Kong/gateway-operator-archive/pull/849)
- Bump Gateway API to v0.6.2 and enable Gateway API conformance testing.
  [Kong/gateway-operator-archive#853](https://github.com/Kong/gateway-operator-archive/pull/853)
- Add `PodTemplateSpec` to `DeploymentOptions` to allow applying strategic merge
  patcher on top of `Pod`s generated by the operator.
  This is a **breaking change** which requires manual porting from `Pods` field
  to `PodTemplateSpec`.
  More info on strategic merge patch can be found in official Kubernetes docs at
  [sig-api-machinery/strategic-merge-patch.md][strategic-merge-patch].
  [Kong/gateway-operator-archive#862](https://github.com/Kong/gateway-operator-archive/pull/862)
- Added `v1beta1` version of the `DataPlane` API, which replaces the `v1alpha1`
  version. The `v1alpha1` version of the API has been removed entirely in favor
  of the new version to reduce maintenance costs.
  [Kong/gateway-operator-archive#905](https://github.com/Kong/gateway-operator-archive/pull/905)

[strategic-merge-patch]: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-api-machinery/strategic-merge-patch.md

### Fixes

- Fixes setting `Affinity` when generating `Deployment`s for `DataPlane`s
  `ControlPlane`s which caused 2 `ReplicaSet`s to be created where the first
  one should already have the `Affinity` set making the update unnecessary.
  [Kong/gateway-operator-archive#894](https://github.com/Kong/gateway-operator-archive/pull/894)

## [v0.5.0]

> Release date: 2023-06-20

### Added

- Added `AddressSourceType` to `DataPlane` status `Address`
  [Kong/gateway-operator-archive#798](https://github.com/Kong/gateway-operator-archive/pull/798)
- Add pod Affinity field to `PodOptions` and support for both `DataPlane` and `ControlPlane`
- Add Kong Gateway enterprise image - `kong/kong-gateway` - to the set of supported
  `DataPlane` images.
  [Kong/gateway-operator-archive#749](https://github.com/Kong/gateway-operator-archive/pull/749)
- Moved pod related options in `DeploymentOptions` to `PodsOptions` and added pod
  labels option.
  [Kong/gateway-operator-archive#742](https://github.com/Kong/gateway-operator-archive/pull/742)
- Added `Volumes` and `VolumeMounts` field in `DeploymentOptions` of `DataPlane`
  specs. Users can attach custom volumes and mount the volumes to proxy container
  of pods in `Deployments` of dataplanes.
  Note: `Volumes` and `VolumeMounts` are not supported for `ControlPlane` specs now.
  [Kong/gateway-operator-archive#681](https://github.com/Kong/gateway-operator-archive/pull/681)
- Added possibility to replicas on `DataPlane` deployments
  This allows users to define `DataPlane`s - without `ControlPlane` - to be
  horizontally scalable.
  [Kong/gateway-operator-archive#737](https://github.com/Kong/gateway-operator-archive/pull/737)
- Added possibility to specify `DataPlane` proxy service type
  [Kong/gateway-operator-archive#739](https://github.com/Kong/gateway-operator-archive/pull/739)
- Added possibility to specify resources through `DataPlane` and `ControlPlane`
  `spec.deployment.resources`
  [Kong/gateway-operator-archive#712](https://github.com/Kong/gateway-operator-archive/pull/712)
- The `DataPlane` spec has been updated with a new field related
  to the proxy service. By using such a field, it is possible to
  specify annotations to be set on the `DataPlane` proxy service.
  [Kong/gateway-operator-archive#682](https://github.com/Kong/gateway-operator-archive/pull/682)

### Changed

- Bumped default ControlPlane image to 2.9.3
  [Kong/gateway-operator-archive#712](https://github.com/Kong/gateway-operator-archive/pull/712)
  [Kong/gateway-operator-archive#719](https://github.com/Kong/gateway-operator-archive/pull/719)
- Bumped default DataPlane image to 3.2.2
  [Kong/gateway-operator-archive#728](https://github.com/Kong/gateway-operator-archive/pull/728)
- Bumped Gateway API to 0.6.1. Along with it, the deprecated `Gateway`
  `scheduled` condition has been replaced by the `accepted` condition.
  [Kong/gateway-operator-archive#618](https://github.com/Kong/gateway-operator-archive/issues/618)
- `ControlPlane` and `DataPlane` specs have been refactored by explicitly setting
  the deployment field (instead of having it inline).
  [Kong/gateway-operator-archive#725](https://github.com/Kong/gateway-operator-archive/pull/725)
- `ControlPlane` and `DataPlane` specs now require users to provide `containerImage`
  and `version` fields.
  This is being enforced in the admission webhook.
  [Kong/gateway-operator-archive#758](https://github.com/Kong/gateway-operator-archive/pull/758)
- Validation for `ControlPlane` and `DataPlane` components no longer has a
  "ceiling", or maximum version. This due to popular demand, but now puts more
  emphasis on the user to troubleshoot when things go wrong. It's no longer
  possible to use a tag that's not semver compatible (e.g. 2.10.0) for these
  components (for instance, a branch such as `main`) without enabling developer
  mode.
  [Kong/gateway-operator-archive#819](https://github.com/Kong/gateway-operator-archive/pull/819)
- `ControlPlane` and `DataPlane` image validation now supports enterprise image
  flavours, e.g. `3.3.0-ubuntu`, `3.2.0.0-rhel` etc.
  [Kong/gateway-operator-archive#830](https://github.com/Kong/gateway-operator-archive/pull/830)

### Fixes

- Fix admission webhook certificates Job which caused TLS handshake errors when
  webhook was being called.
  [Kong/gateway-operator-archive#716](https://github.com/Kong/gateway-operator-archive/pull/716)
- Include leader election related role when generating `ControlPlane` RBAC
  manifests so that Gateway Discovery can be used by KIC.
  [Kong/gateway-operator-archive#743](https://github.com/Kong/gateway-operator-archive/pull/743)

## [v0.4.0]

> Release date: 2022-01-25

### Added

- Added machinery for ControlPlanes to communicate with DataPlanes
  directly via Pod IPs. The Admin API has been removed from the LoadBalancer service.
  [Kong/gateway-operator-archive#609](https://github.com/Kong/gateway-operator-archive/pull/609)
- The Gateway Listeners status is set and kept up to date by the Gateway controller.
  [Kong/gateway-operator-archive#627](https://github.com/Kong/gateway-operator-archive/pull/627)

## [v0.3.0]

> Release date: 2022-11-30

**Maturity: ALPHA**

### Changed

- Bumped DataPlane default image to 3.0.1
  [Kong/gateway-operator-archive#561](https://github.com/Kong/gateway-operator-archive/pull/561)

### Added

- Gateway statuses now include all addresses from their DataPlane Service.
  [Kong/gateway-operator-archive#535](https://github.com/Kong/gateway-operator-archive/pull/535)
- DataPlane Deployment strategy enforced as RollingUpdate.
  [Kong/gateway-operator-archive#537](https://github.com/Kong/gateway-operator-archive/pull/537)

### Fixes

- Regenerate DataPlane's TLS secret upon deletion
  [Kong/gateway-operator-archive#500](https://github.com/Kong/gateway-operator-archive/pull/500)
- Gateway statuses no longer list cluster IPs if their DataPlane Service is a
  LoadBalancer.
  [Kong/gateway-operator-archive#535](https://github.com/Kong/gateway-operator-archive/pull/535)

## [v0.2.0]

> Release date: 2022-10-26

**Maturity: ALPHA**

### Added

- Updated default Kong version to 3.0.0
- Updated default Kubernetes Ingress Controller version to 2.7
- Update DataPlane and ControlPlane Ready condition when underlying Deployment
  changes Ready condition
  [Kong/gateway-operator-archive#451](https://github.com/Kong/gateway-operator-archive/pull/451)
- Update DataPlane NetworkPolicy to match KONG_PROXY_LISTEN and KONG_ADMIN_LISTEN
  environment variables set in DataPlane
  [Kong/gateway-operator-archive#473](https://github.com/Kong/gateway-operator-archive/pull/473)
- Added Container image and version validation for ControlPlanes and DataPlanes.
  The operator now only supports the Kubernetes-ingress-controller (2.7) as
  the ControlPlane, and Kong (3.0) as the DataPlane.
  [Kong/gateway-operator-archive#490](https://github.com/Kong/gateway-operator-archive/pull/490)
- DataPlane resources get a new `Status` field: `Addresses` which will contain
  backing service addresses.
  [Kong/gateway-operator-archive#483](https://github.com/Kong/gateway-operator-archive/pull/483)

## [v0.1.1]

> Release date:  2022-09-24

**Maturity: ALPHA**

### Added

- `HTTPRoute` support was added. If version of control plane image is at
  least 2.6, the `Gateway=true` feature gate is enabled, so the
  control plane can pick up the `HTTPRoute` and configure it on data plane.
  [Kong/gateway-operator-archive#302](https://github.com/Kong/gateway-operator-archive/pull/302)

## [v0.1.0]

> Release date: 2022-09-15

**Maturity: ALPHA**

This is the initial release which includes basic functionality at an alpha
level of maturity and includes some of the fundamental APIs needed to create
gateways for ingress traffic.

### Initial Features

- The `GatewayConfiguration` API was added to enable configuring `Gateway`
  resources with the options needed to influence the configuration of
  the underlying `ControlPlane` and `DataPlane` resources.
  [Kong/gateway-operator-archive#43](https://github.com/Kong/gateway-operator-archive/pull/43)
- `GatewayClass` support was added to delineate which `Gateway` resources the
  operator supports.
  [Kong/gateway-operator-archive#22](https://github.com/Kong/gateway-operator-archive/issues/22)
- `Gateway` support was added: used to create edge proxies for ingress traffic.
  [Kong/gateway-operator-archive#6](https://github.com/Kong/gateway-operator-archive/issues/6)
- The `ControlPlane` API was added to deploy Kong Ingress Controllers which
  can be attached to `DataPlane` resources.
  [Kong/gateway-operator-archive#5](https://github.com/Kong/gateway-operator-archive/issues/5)
- The `DataPlane` API was added to deploy Kong Gateways.
  [Kong/gateway-operator-archive#4](https://github.com/Kong/gateway-operator-archive/issues/4)
- The operator manages certificates for control and data plane communication
  and configures mutual TLS between them. It cannot yet replace expired
  certificates.
  [Kong/gateway-operator-archive#103](https://github.com/Kong/gateway-operator-archive/issues/103)

### Known issues

When deploying the gateway-operator through the bundle, there might be some
leftovers from previous operator deployments in the cluster. The user needs to delete all the cluster-wide leftovers
(clusterrole, clusterrolebinding, validatingWebhookConfiguration) before
re-installing the operator through the bundle.

[v1.6.2]: https://github.com/Kong/gateway-operator/compare/v1.6.1..v1.6.2
[v1.6.1]: https://github.com/Kong/gateway-operator/compare/v1.6.0..v1.6.1
[v1.6.0]: https://github.com/Kong/gateway-operator/compare/v1.5.1..v1.6.0
[v1.5.1]: https://github.com/Kong/gateway-operator/compare/v1.5.0..v1.5.1
[v1.5.0]: https://github.com/Kong/gateway-operator/compare/v1.4.2..v1.5.0
[v1.4.2]: https://github.com/Kong/gateway-operator/compare/v1.4.1..v1.4.2
[v1.4.1]: https://github.com/Kong/gateway-operator/compare/v1.4.0..v1.4.1
[v1.4.0]: https://github.com/Kong/gateway-operator/compare/v1.3.0..v1.4.0
[v1.3.0]: https://github.com/Kong/gateway-operator/compare/v1.2.3..v1.3.0
[v1.2.3]: https://github.com/Kong/gateway-operator/compare/v1.2.2..v1.2.3
[v1.2.2]: https://github.com/Kong/gateway-operator/compare/v1.2.1..v1.2.2
[v1.2.1]: https://github.com/Kong/gateway-operator/compare/v1.2.0..v1.2.1
[v1.2.0]: https://github.com/Kong/gateway-operator/compare/v1.1.0..v1.2.0
[v1.1.0]: https://github.com/Kong/gateway-operator/compare/v1.0.3..v1.1.0
[v1.0.3]: https://github.com/Kong/gateway-operator/compare/v1.0.2..v1.0.3
[v1.0.2]: https://github.com/Kong/gateway-operator/compare/v1.0.1..v1.0.2
[v1.0.1]: https://github.com/Kong/gateway-operator/compare/v1.0.0..v1.0.1
[v1.0.0]: https://github.com/Kong/gateway-operator/compare/v0.7.0..v1.0.0
[v0.7.0]: https://github.com/Kong/gateway-operator/compare/v0.6.0..v0.7.0
[v0.6.0]: https://github.com/Kong/gateway-operator/compare/v0.5.0..v0.6.0
[v0.5.0]: https://github.com/Kong/gateway-operator/compare/v0.4.0..v0.5.0
[v0.4.0]: https://github.com/Kong/gateway-operator/compare/v0.3.0..v0.4.0
[v0.3.0]: https://github.com/Kong/gateway-operator/compare/v0.2.0..v0.3.0
[v0.2.0]: https://github.com/Kong/gateway-operator/compare/v0.1.0..v0.2.0
[v0.1.1]: https://github.com/Kong/gateway-operator/compare/v0.0.1..v0.1.1
[v0.1.0]: https://github.com/Kong/gateway-operator/compare/v0.0.0..v0.1.0
