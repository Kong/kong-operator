# Features

The Kong Operator (KO) enables provisioning and lifecycle management
of [Kong Gateways][gw] on [Kubernetes][k8s] and also includes automation
for various related components and configuration management to deploy
the Kong Gateway in various [deployment topologies][tops].

The subsections that follow are a list of the current [capabilities][caps] for
this operator which are actively developed & maintained.

If you're interested in tracking or creating proposals for new features and
capabilities, please see our [KEPs][keps]. If you'd like to track the progress
of development for active features or get involved and contribute in them, check
our Github [milestones][mst] and [issues][iss].

## Kong Gateway Deployment & Configuration Management

We support the ability to create (and destroy) `Gateways` using the Kubernetes
resource of the same name from [Kubernetes Gateway API][gwapi]. A `Gateway` is
comprised of multiple sub-components "under the hood" including the [Kong
Gateway][gw] (implemented via the [DataPlane API][dapi]) and the [Kong
Kubernetes Ingress Controller (KIC)][kic] (implemented via the [ControlPlane
API][capi]).

Declarative configuration management for `Gateways` and the underlying
sub-components is provided by our [GatewayConfiguration API][gwcfg] which
includes (non-exhaustively) configuration options for the `ControlPlane`
and `DataPlane`. The `GatewayConfiguration` API can be attached to any
number of `Gateways`, enabling lifecycle management for a single `Gateway`
or for a group of `Gateways` from a single configuration. Multiple groups of
`Gateways` which need different and independent configuration can be managed
using multiple `GatewayConfigurations`.

The `DataPlane` (e.g. the [Kong Gateway][gw]) can be configured and deployed
according to its available [deployment topologies][tops] using the
`GatewayConfiguration` API. The following topologies are currently supported:

- [Kong Gateway][gw] in [dbless mode][dbl] + [Kong Ingress Controller (KIC)][kic]

More configurations and topologies may become available in future releases.

> **Note**: We currently don't support [traditional mode][trd] for the Kong
> Gateway as managing an independent database server in a Kubernetes cluster
> is non-trivial and out of scope for community support.

> **Note**: While we don't currently support the Kong Gateway configured in
> [hybrid mode control-plane][hybrc] configuration, we do support
> [hybrid mode data-plane][hybrd] configuration using the `DataPlane` API. See
> below sections for details.

## Gateway Upgrades & Downgrades

We support user-triggered upgrades and downgrades of the [ControlPlane][capi]
and [DataPlane][dapi] sub-components of `Gateways` by configuring the
[corresponding versioning information][sp] in the [GatewayConfiguration][gwcfg].

Upgrades and downgrades of sub-components include transitions where existing
routes do not fail as automation is in place to "smoothly" wait for health
and `DataPlane` configuration before traffic is pivoted to the new version.

## Gateway Horizontal Scaling

We support user-triggered scaling of `DataPlanes` for `Gateway` deployments,
where the number of `Pods` can be adjusted up and down as needed according to
`DataPlane` resource utilization and traffic.

> **Warning**: Currently this only affects the `DataPlane` `Pod` scaling. `ControlPlane`
> `Pod` scaling is a consideration for future releases.

## Kong Hybrid Mode DataPlane

The [Kong Gateway][gw] can be deployed in [hybrid mode][hybr] which allows
multiple gateways to be joined together for scaling and resiliency. This
operator supports attaching a [hybrid mode dataplane][hybrd] to an [existing
hybrid mode [control-plane][hybrc] using the `DataPlane` API. A quick start for
this feature can be in the [docs][quick-start-konnect].

## Kong AI Gateway

We provide an `AIGateway` resource which can be used to deploy the [Kong
Gateway][gw] with our [AI Plugins][aiplugins] automatically configured and
enabled, to provide managed access to various AI models such as those provided
by OpenAI (e.g. ChatGPT).

> **Note**: this feature is currently considered _experimental_ and is not
> enabled by default. The CRD must be deployed manually (it is not provided as
> part of our `kustomize` bundle):
>
> ```bash
> kubectl apply -f config/crd/bases/gateway-operator.konghq.com_aigateways.yaml
> ```
>
> Then see our `config/samples/aigateway.yaml` example to get started.

[hybr]:https://developer.konghq.com/gateway/hybrid-mode/
[keps]:https://github.com/kong/kong-operator/tree/main/keps
[k8s]:https://kubernetes.io
[caps]:https://sdk.operatorframework.io/docs/overview/operator-capabilities/
[mst]:https://github.com/kong/kong-operator/milestone
[iss]:https://github.com/kong/kong-operator/issues
[sp]:https://pkg.go.dev/github.com/kong/kong-operator@main/api/v1beta1#GatewayConfigurationSpec
[gwapi]:https://gateway-api.sigs.k8s.io
[gw]:https://github.com/kong/kong
[gwcfg]:https://pkg.go.dev/github.com/kong/kong-operator@main/api/v1beta1#GatewayConfiguration
[tops]:https://developer.konghq.com/gateway/deployment-topologies/
[dapi]:https://pkg.go.dev/github.com/kong/kong-operator@main/api/v1beta1#DataPlane
[kic]:https://github.com/kong/kubernetes-ingress-controller
[capi]:https://pkg.go.dev/github.com/kong/kong-operator@main/api/v1alpha1#ControlPlane
[dbl]:https://developer.konghq.com/gateway/db-less-mode/
[trd]:https://developer.konghq.com/gateway/traditional-mode/
[hybrd]:https://developer.konghq.com/gateway/hybrid-mode/#install-and-start-data-planes
[hybrc]:https://developer.konghq.com/gateway/hybrid-mode/#set-up-the-control-plane
[aiplugins]:https://konghq.com/products/kong-ai-gateway
[quick-start-konnect]:https://developer.konghq.com/operator/install/
