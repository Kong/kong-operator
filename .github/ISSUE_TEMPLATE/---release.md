




- [ ] Update the official documentation at [github.com/Kong/docs.konghq.com][docs_repo]
  - [ ] Run post processing script for `docs/api-reference.md`, providing a tagged version of CRD reference from docs repo as an argument, e.g. `app/_src/gateway-operator/reference/custom-resources/1.2.x.md`. This will add the necessary skaffolding so that the reference is rendered correctly on docs.konghq.com.
    Example: `${GATEWAY_OPERATOR_REPO}/scripts/apidocs-gen/post-process-for-konghq.sh ~/docs.konghq.com/app/_src/gateway-operator/reference/custom-resources/1.2.x.md`
  - NOTE: [CLI configuration options docs][cli_ref_docs] should be updated when releasing KGO EE as that's the source of trutgh for those.
    The reason for this is that KGO EE configuration flags are a superset of OSS flags.
- [ ] Proceed to release KGO EE as it relies on OSS releases.

[docs_repo]: https://github.com/Kong/docs.konghq.com/
[cli_ref_docs]: https://docs.konghq.com/gateway-operator/latest/reference/cli-arguments/

## Verify default hardcoded versions

The package [internal/consts][consts-pkg] contains a list of default versions for the operator. These versions should be updated to match the new release. The example consts to look for:

- `DefaultDataPlaneTag`
- `DefaultControlPlaneTag`
- `WebhookCertificateConfigBaseImage`

## Troubleshooting

### The release needs to be started again with the same tag

If the release workflow needs to be started again with the same input version, the release branch needs to be deleted. The release branch is created by the CI and it's named `release/v<version>`. For example, if the release version is `v0.1.0`, the release branch will be `release/v0.1.0`.

It's only safe to start a release workflow with the version that was previously used if:

- The release PR to the gateway-operator repo is not merged yet.
- The external hub PRs are not merged yet.
- The tag that matches the release version does not exist.

Otherwise, if the above conditions are not meet, the release cannot be restarted. A new release needs to be started with an input version that would be next in semantic versioning.

Steps:

1. Delete the `release/v<version>` branch.
2. Delete the PR created by a release workflow.
3. Update the repository with the correct changes.
4. Start a new release workflow run.

[releases]: https://github.com/Kong/gateway-operator/releases
[release-action]: https://github.com/Kong/gateway-operator/actions/workflows/release.yaml
[consts-pkg]: https://github.com/Kong/gateway-operator/blob/main/internal/consts/consts.go
[operator-hub-community]: https://github.com/k8s-operatorhub/community-operators
[kgo-prs]: https://github.com/Kong/gateway-operator/pulls
