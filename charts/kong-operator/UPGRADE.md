# Upgrade considerations

New versions of this chart may add significant new functionality or
deprecate/entirely remove old functionality. This document covers how and why
users should update their chart configuration to take advantage of new features
or migrate away from deprecated features.

In general, breaking changes deprecate their old features before removing them
entirely. While support for the old functionality remains, the chart will show
a warning about the outdated configuration when running
`helm install/status/upgrade`.

Note that not all versions contain breaking changes. If a version is not
present in the table of contents, it requires no version-specific changes when
upgrading from a previous version.

## Upgrading from KGO - Kong Gateway Operator chart

If you're upgrading from KGO - Kong Gateway Operator chart you will need to
update the CRDs manually since [Helm does not manage CRD updates][helm_crd_update].

This can be done by running the following command:

```sh
kustomize build github.com/kong/kong-operator/config/crd/gateway-operator | kubectl apply --server-side -f -
```

[helm_crd_update]: https://helm.sh/docs/chart_best_practices/custom_resource_definitions/

## Updating operator version

The operator version is following [SemVer][semver].
This means that users should not expect breaking changes without a major version change.

Any changes requiring manual user action will be called out in operator [release notes][ko_release_notes].

[semver]: https://semver.org/
[ko_release_notes]: https://github.com/Kong/kong-operator/blob/main/CHANGELOG.md

## Updates to CRDs

Helm installs CRDs at initial install but [does not update them after][hip0011].
Some chart releases include updates to CRDs that must be applied to successfully
upgrade. Because Helm does not handle these updates, you must manually apply
them before upgrading your release.

[hip0011]: https://github.com/helm/community/blob/main/hips/hip-0011.md

For example, upgrading Kong Operator CRDs to v2.0.1 requires
running:

```sh
kustomize build github.com/Kong/kong-operator/config/crd/gateway-operator?ref=v2.0.1 | kubectl apply -f -
```

Upgrading [Gateway API][gwapi] to v1.5.1 requires running:

```sh
kustomize build github.com/kubernetes-sigs/gateway-api/config/crd\?ref=v1.5.1 | kubectl apply -f -
```

[gwapi]: https://github.com/kubernetes-sigs/gateway-api/

## CRD field descriptions

Starting with chart version 1.5.0, the Kong Operator CRDs shipped with this
chart (`ko-crds`) no longer carry per-field `description` doc strings. With them the Helm release manifest
grows past the 1MiB `Secret` size limit, which makes `helm install`/`helm upgrade`
fail. As a result, `kubectl explain` does not print field documentation for
these CRDs.

If you want the field documentation in your cluster, apply the full CRDs for
your operator version (the chart's `appVersion`) after every `helm install` or
`helm upgrade`. The chart's CRDs differ from the full ones only by the doc
strings, so server-side apply with a separate field manager adds the
descriptions and leaves the Helm-managed fields, including the conversion
webhook configuration, untouched:

```sh
# Set to the appVersion of the installed chart, e.g.
# helm get metadata <release> -n <namespace> -o json | jq -r .appVersion
KO_VERSION=<version>

kustomize build "github.com/Kong/kong-operator/config/crd/kong-operator?ref=v${KO_VERSION}" | \
  kubectl apply --server-side --field-manager=kong-operator-crd-docs -f -
```

Do not install the CRDs from `config/crd/kong-operator` in place of the chart's
`ko-crds` (i.e. with `ko-crds.enabled=false`): they do not contain the
conversion webhook configuration that the chart templates for your release.
