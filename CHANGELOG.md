# Changelog

## Table of Contents

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

### Fixes

- Fixes an issue where managed `Gateway`s controller wasn't able to reduce
  the created `DataPlane` objects when too many have been created.
  [#43](https://github.com/Kong/gateway-operator/pull/43)
- `Gateway` controller will no longer set `DataPlane` deployment's replicas
  to default value when `DataPlaneOptions` in `GatewayConfiguration` define
  scaling strategy. This effectively allows users to use `DataPlane` horizontal
  autoscaling with `GatewayConfiguration` as the generated `DataPlane` deployment
  will no longer be rejected.
  [#79](https://github.com/Kong/gateway-operator/pull/79)

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

[v1.2.1]: https://github.com/Kong/gateway-operator-archive/compare/v1.2.0..v1.2.1
[v1.2.0]: https://github.com/Kong/gateway-operator-archive/compare/v1.1.0..v1.2.0
[v1.1.0]: https://github.com/Kong/gateway-operator-archive/compare/v1.0.3..v1.1.0
[v1.0.3]: https://github.com/Kong/gateway-operator-archive/compare/v1.0.2..v1.0.3
[v1.0.2]: https://github.com/Kong/gateway-operator-archive/compare/v1.0.1..v1.0.2
[v1.0.1]: https://github.com/Kong/gateway-operator-archive/compare/v1.0.0..v1.0.1
[v1.0.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.7.0..v1.0.0
[v0.7.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.6.0..v0.7.0
[v0.6.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.5.0..v0.6.0
[v0.5.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.4.0..v0.5.0
[v0.4.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.3.0..v0.4.0
[v0.3.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.2.0..v0.3.0
[v0.2.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.1.0..v0.2.0
[v0.1.1]: https://github.com/Kong/gateway-operator-archive/compare/v0.0.1..v0.1.1
[v0.1.0]: https://github.com/Kong/gateway-operator-archive/compare/v0.0.0..v0.1.0
