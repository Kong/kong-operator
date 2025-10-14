# [Kong Operator][docs]

<img src="./logo/logo.png" alt="KGO logo" title="Kong Operator" height="150" width="150" />

Kong Operator is a [Kubernetes Operator][operator-concept] that can manage
your Kong Ingress Controller, Kong Gateway Data Planes, or both together when running
on Kubernetes.

With Kong Operator, users can:

* Deploy and configure Kong Gateway services.
* Customise deployments using `PodTemplateSpec` to:
  * [Deploy sidecars][docs_sidecar],
  * [Set image][docs_dataplane_image],
  * [And much more][docs_podtemplatespec].
* Upgrade Data Planes using a [rolling restart][docs_upgrade_rolling] or [blue/green deployments][docs_upgrade_bg].
* Configure [auto scaling on Data Planes][docs_autoscaling].

[docs_sidecar]: https://developer.konghq.com/operator/dataplanes/how-to/deploy-sidecars/
[docs_dataplane_image]: https://developer.konghq.com/operator/dataplanes/how-to/set-dataplane-image/
[docs_podtemplatespec]: https://developer.konghq.com/operator/dataplanes/reference/podtemplatespec/
[docs_upgrade_rolling]: https://developer.konghq.com/operator/dataplanes/upgrade/gateway/rolling/
[docs_upgrade_bg]: https://developer.konghq.com/operator/dataplanes/upgrade/gateway/blue-green/
[docs_autoscaling]: https://developer.konghq.com/operator/dataplanes/reference/autoscale-workloads/

## Current Features

The following features are considered supported:

* Kong Gateway Deployment & Configuration Management using the [Gateway API][gwapi]
* Creation of [Kong Gateways][konggw] using the `DataPlane` API
* [Kong Gateways][konggw] upgrades, downgrades and autoscaling
* Creation of [Kong Ingress Controller][kic] instances using the `ControlPlane` API
* Hybrid Mode Attachment using the `DataPlane` API
* Configuration and management of `AIGateway`s (experimental feature)

See our [Features Page](/FEATURES.md) for details on these capabilities.

## API stability

The operator provides 2 APIs:

* YAML / manifests API which users use to apply their manifests against Kubernetes clusters.
* Go API through types exported under [api/](https://github.com/kong/kong-operator/tree/main/api)
  and other exported packages.

This project:

* Follows [Kubernetes API versioning][k8s_api_versioning] for the YAML API.
  * This is considered part of the user contract.
* Tries to not break users implementing against operator's Go API but does not
  offer a non breaking guarantee.

[k8s_api_versioning]: https://kubernetes.io/docs/reference/using-api/#api-versioning

## Quick Start and documentation

If you are eager to start with the operator, you can visit the quick start [section][docsqs]
of the documentation. Alternatively, the complete [docs][docs] provide a full and
detailed description of how to thoroughly use this project.

## Container images

### Release images

Release builds can be found on Docker Hub in [kong/kong-operator repository][dockerhub-ko].

At the moment we're providing images for:

* Linux `amd64`
* Linux `arm64`

[dockerhub-ko]: https://hub.docker.com/r/kong/kong-operator

### `main` branch builds

Nightly pre-release builds of the `main` branch are available from the
[kong/nightly-kong-operator repository][dockerhub-ko-nightly] hosted on Docker Hub.

[dockerhub-ko-nightly]: https://hub.docker.com/r/kong/nightly-kong-operator

## Development

### Prerequisites

In order to build the operator you'll have to have Go installed on your machine.
In order to do so, follow the instructions on its website[go-dev-site].

### Build process

Building the operator should be as simple as running:

```console
make build
```

This `Makefile` target will take care of everything from generating client side code,
generating Kubernetes manifests, downloading the dependencies and the tools used
in the build process and finally, it will build the binary.

After this step has finished successfully you should see the operator's binary `bin/manager`.

You can also run it directly via `make run` which will run the operator on your
machine against the cluster that you have configured via your `KUBECONFIG`.

### Adding new CRDs

Whenever you add a new CRD:

* Ensure that it is included in project's [`PROJECT`](./PROJECT) file. This is necessary for creation of
  a bundle for external hubs like [Operator Hub's community operators][community-operators].
* Annotate the CRD and any new type it depends on with the right markers to make sure it will be included
  in the generated documentation. See the markers used in scripts/crds-generator for reference.

[community-operators]: https://github.com/k8s-operatorhub/community-operators/

## Seeking Help

Please search through the posts on the [discussions page][disc] as it's likely
that another user has run into the same problem. If you don't find an answer,
please feel free to post a question.

If you've found a bug, please [open an issue][issues].

For a feature request, please open an issue using the feature request template.

You can also talk to the developers behind Kong in the [#kong][slack] channel on
the Kubernetes Slack server.

## Community Meetings

You can join bi-weekly meetups hosted by [Kong][kong] to ask questions, provide
feedback, or just to listen and hang out.

See the [Online Meetups Page][kong-meet] to sign up and receive meeting invites
and [Zoom][zoom] links.

[kong]:https://konghq.com
[konggw]:https://github.com/kong/kong
[kic]:https://github.com/kong/kubernetes-ingress-controller
[gwapi]:https://github.com/kubernetes-sigs/gateway-api
[go-dev-site]: https://go.dev/
[disc]:https://github.com/kong/kong-operator/discussions
[issues]:https://github.com/kong/kong-operator/issues
[slack]:https://kubernetes.slack.com/messages/kong
[kong-meet]:https://konghq.com/online-meetups/
[zoom]:https://zoom.us
[docs]:https://developer.konghq.com/operator/
[docsqs]:https://developer.konghq.com/operator/dataplanes/get-started/kic/install/
[operator-concept]:https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
