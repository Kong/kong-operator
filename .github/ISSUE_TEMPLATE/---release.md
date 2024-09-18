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
- [ ] If the new release is a new major/minor then branch off of `main` and create a branch `release/N.M.x`, e.g. 
- [ ] From [GitHub release action][release-action], start a new workflow run:
  - Set the `Use workflow from` to the release branch: e.g. `release/1.2.x`
  - Set the `release` input set to the target version (e.g. `v1.2.0`).
- [ ] Wait for the workflow to complete.
- [ ] The CI should create a PR in the [Gateway Operator][kgo-prs] repo that syncs the release branch to the base branch (either `main` or `release/N.M.x`, e.g. `release/1.2.x`) branch. Merge it.
- [ ] After the PR is merged, a new release should be created automatically. Check the [releases][releases] page. The release has to be marked manually as `latest` if this is the case.
- [ ] Update the official documentation at [github.com/Kong/docs.konghq.com][docs_repo]
  - [ ] Run post processing script for `docs/api-reference.md`, providing a tagged version of CRD reference from docs repo as an argument, e.g. `app/_src/gateway-operator/reference/custom-resources/1.2.x.md`.
    This will add the necessary skaffolding so that the reference is rendered correctly on docs.konghq.com.

    Example:
    ```
    ${GATEWAY_OPERATOR_REPO}/scripts/apidocs-gen/post-process-for-konghq.sh ~/docs.konghq.com/app/_src/gateway-operator/reference/custom-resources/1.2.x.md
    ```

  - NOTE: [CLI configuration options docs][cli_ref_docs] should be updated when releasing KGO EE as that's the source of truth for those.
    The reason for this is that KGO EE configuration flags are a superset of OSS flags.

- [ ] Proceed to release KGO EE as it relies on OSS releases.

**Only for major and minor releases**:

- [ ] When the release tag is created add a test case in [upgrade E2E test][helm_upgrade_test] with just published tag so that an upgrade path from previous major/minor version is tested.
  - [ ] When the release contains breaking changes which precludes an automated upgrade make sure to add a comment to this test for future readers.
- [ ] Schedule a retro meeting and invite the team. Link the invite in the [retro notes](https://docs.google.com/document/d/15gDtl425zyttbDwA8qQrh5yBgTD5OpnhjOquqfSJUx4/edit#heading=h.biunbyheelys)


[docs_repo]: https://github.com/Kong/docs.konghq.com/
[cli_ref_docs]: https://docs.konghq.com/gateway-operator/latest/reference/cli-arguments/
[helm_upgrade_test]: https://github.com/Kong/gateway-operator/blob/9f33d27ab875b91e50d7e750b45a293c1395da2d/test/e2e/test_upgrade.go

## Verify default hardcoded versions

> **NOTE**: These versions should be automatically updated via Renovate.
> As part of the release workflow please verify that this is indeed the case and the automation still works.

The packages [internal/consts][consts-pkg] and [pkg/versions][versions-pkg] contains a list of default versions for the operator.
These versions should be updated to match the new release. The example consts to look for:

- `DefaultDataPlaneTag`
- `DefaultControlPlaneVersion`
- `WebhookCertificateConfigBaseImage`

## GitHub PAT

**Next expiration date**: 2024-10-02

The release workflow uses @team-k8s-bot's GitHub PAT to create a GitHub release and PRs related to it.
It's named `Kong Gateway operator release pipeline` and is stored in `PAT_GITHUB`
GitHub repository secret to give workflows access to it. It's always generated with 1-year expiration date.

If you find it's expired, make sure to generate a new one and update the `PAT_GITHUB` secret as well as its 1Pass item
`Github team k8s bot - PAT - Kong Gateway operator release token` for redundancy.

## Troubleshooting

### The release needs to be started again with the same tag

If the release workflow needs to be started again with the same input version, the release branch needs to be deleted. The release branch is created by the CI and it's named `release/v<version>`. For example, if the release version is `v0.1.0`, the release branch will be `release/v0.1.0`.

It's only safe to start a release workflow with the version that was previously used if:

- The release PR to the gateway-operator repo is not merged yet.
- The external hub PRs are not merged yet.
- The tag that matches the release version does not exist.

Otherwise, if the above conditions are not meet, the release cannot be restarted. A new release needs to be started with an input version that would be next in semantic versioning.

Steps:

1. Delete the PR created by a release workflow.
1. Update the repository with the correct changes.
1. Start a new release workflow run.

[releases]: https://github.com/Kong/gateway-operator/releases
[release-action]: https://github.com/Kong/gateway-operator/actions/workflows/release.yaml
[consts-pkg]: https://github.com/Kong/gateway-operator/blob/main/pkg/consts/consts.go
[versions-pkg]: https://github.com/Kong/gateway-operator/blob/main/internal/versions/
[kgo-prs]: https://github.com/Kong/gateway-operator/pulls
