---
name: "🚀 Release"
about: 'Tracking a new release of the Kong Kubernetes Gateway Operator'
title: ''
labels: 'area/release'
assignees: ''

---

## Steps

This release pipeline is under continous improvement. If you encounter any problem, please refer to the [troubleshooting](#troubleshooting) section of this document.
If the troubleshooting section does not contain the answer to the problem you encountered, please create an issue to improve either the pipeline (if the problem is a bug), or this document (if the problem is caused by human error).

- [ ] Check the existing [releases][releases] and determine the next version number.
- [ ] Check [default versions](#verify-default-hardcoded-versions) of images (see below).
- [ ] Check the `CHANGELOG.md` and update it with the new version number. Make sure the log is up to date.
- [ ] Ensure that all generators have run properly (e.g. `make generate manifests`) so that updates to things like CRDs are handled for the release, double check that all manifests from `config/samples/` still work as intended.
- [ ] Ensure GitHub PAT is still valid (see [GitHub PAT](#github-pat) below).
- [ ] From [GitHub release action][release-action], start a new workflow run with the `release` input set to the release tag (e.g. `v0.1.0`).
- [ ] Wait for the workflow to complete.
- [ ] The CI should create a PR in the [Gateway Operator][kgo-prs] repo that syncs the release branch to the `main` branch. Merge it.
- [ ] After the PR is merged, a new release should be created automatically. Check the [releases][releases] page. The release has to be marked manually as `latest` if this is the case.
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

## GitHub PAT

**Next expiration date**: 2024-10-02

The release workflow uses @team-k8s-bot's GitHub PAT to create a GitHub release and PRs related to it (in [KGO docs][operator-docs],
[OperatorHub][community-operators], etc.). It's named `Kong Gateway operator release pipeline` and is stored in `PAT_GITHUB`
GitHub repository secret to give workflows access to it. It's always generated with 1-year expiration date.

If you find it's expired, make sure to generate a new one and update the `PAT_GITHUB` secret as well as its 1Pass item
`Github team k8s bot - PAT - Kong Gateway operator release token` for redundancy.

[operator-docs]: https://github.com/Kong/gateway-operator-docs
[community-operators]: https://github.com/Kong/k8s-operatorhub-community-operators

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
