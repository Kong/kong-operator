# Feature-gated samples

Samples in this directory need setup beyond what a stock Kong Operator install
gives you — an extra CRD channel, a feature gate that is off by default, or
both. They are kept in a subdirectory on purpose: `make test.samples` iterates
`config/samples/*.yaml` and does not recurse, so these are not applied and
deleted in CI. Treat them as reference material you apply by hand after doing
the setup each one describes.

Every sample here carries its prerequisites in a header comment. The table is a
summary.

| Sample | Needs |
|---|---|
| [`kong-service-facade.yaml`](kong-service-facade.yaml) | The `ingress-controller-incubator` CRD channel, plus the `KongServiceFacade` feature gate |
| [`ingress-broken-plugin-fallback.yaml`](ingress-broken-plugin-fallback.yaml) | The `FallbackConfiguration` feature gate |
| [`gateway-httproute-broken-plugin-fallback.yaml`](gateway-httproute-broken-plugin-fallback.yaml) | The `FallbackConfiguration` feature gate |

## Feature gates

Gates are set per ControlPlane, either directly on a `ControlPlane` or, for an
operator-managed Gateway, under `GatewayConfiguration.spec.controlPlaneOptions`:

```yaml
featureGates:
- name: FallbackConfiguration
  state: enabled
```

Defaults live in
[`ingress-controller/pkg/manager/config/feature_gates_keys.go`](../../../ingress-controller/pkg/manager/config/feature_gates_keys.go).

## Extra CRDs

`config/crd/kustomization.yaml` only pulls in the `kong-operator` channel, so
`make install.crds` does not install `KongServiceFacade`. Install that channel
separately:

```shell
kubectl apply --server-side -k config/crd/ingress-controller-incubator
```

## A note on the "broken plugin" samples

Those two samples deliberately contain a misconfigured KongPlugin, to show that
`FallbackConfiguration` keeps the rest of the configuration working instead of
rejecting the whole config update. The operator ships a validating webhook
(`kong.validations.kong.konghq.com`) that checks KongPlugin config against the
Kong schema, and when it is active it rejects the broken plugin at admission —
which is the desired production behaviour, but means you will not see the
fallback path. To exercise the fallback, apply these against an install whose
KongPlugin validating webhook is disabled.
