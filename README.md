# [Kong Gateway Operator](https://docs.konghq.com/gateway-operator/latest/)

<img src="./logo/logo.png" alt="KGO logo" title="Kong Gateway Operator" height="150" width="150" />

Kong Gateway Operator is a [Kubernetes Operator][operator-concept] that can manage your Kong Ingress Controller, Kong Gateway Data Planes, or both together when running on Kubernetes.

With Kong Gateway Operator, users can:

* Deploy and configure Kong Gateway services
* Customise deployments using PodTemplateSpec to deploy sidecars, set node affinity and more.
* Upgrade Data Planes using a rolling restart or blue/green deployments

## Current Features

The following features are considered supported:

- Kong Gateway Deployment & Configuration Management using the [Gateway API][gwapi]
- Creation of [Kong Gateways][konggw] using the `DataPlane` API
- [Kong Gateways][konggw] upgrades, downgrades and autoscaling
- Creation of [Kong Ingress Controller][kic] instances using the `ControlPlane` API
- Hybrid Mode Attachment using the `DataPlane` API
- Configuration and management of `AIGateway`s (experimental feature)

See our [Features Page](/FEATURES.md) for details on these capabilities.

## Quick Start and documentation

If you are eager to start with the operator, you can visit the quick start [section][docsqs]
of the documentation. Alternatively, the complete [docs][docs] provide a full and detailed
description of how to thoroughly use this project.

## Development

### Prerequisites

In order to build the operator you'll have to have Go installed on your machine.
In order to do so, follow instructions on [its website][go-dev-site].

### Build process

Building the operator should be as simple as running:

```console
make build
```

This `Makefile` target will take care of everything from generating client side code,
generating Kubernetes manifests, downloading the dependencies and the tools used
in the build process and finally it will build the binary.

After this step has finished successfully you should see the operator's binary `bin/manager`.

You can also run it directly via `make run` which will run the operator on your
machine against the cluster that you have configured via your `KUBECONFIG`.

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
[disc]:https://github.com/kong/gateway-operator/discussions
[issues]:https://github.com/kong/kubernetes-ingress-controller/issues
[slack]:https://kubernetes.slack.com/messages/kong
[kong-meet]:https://konghq.com/online-meetups/
[zoom]:https://zoom.us
[docs]:https://docs.konghq.com/gateway-operator/latest/
[docsqs]:https://docs.konghq.com/gateway-operator/latest/get-started/kic/install/
[operator-concept]:https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
