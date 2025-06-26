---
name: "🚀 Release"
about: 'Tracking a new release of the Kong Operator'
title: ''
labels: 'area/release'
assignees: ''

---

## Steps

This release pipeline is under continuous improvement. If you encounter any problem, please refer to the [troubleshooting](#troubleshooting) section of this document.
If the troubleshooting section does not contain the answer to the problem you encountered, please create an issue to improve either the pipeline (if the problem is a bug), or this document (if the problem is caused by human error).

- [ ] Check the existing [releases][releases] and determine the next version number.
- [ ] Check [default versions](#verify-default-hardcoded-versions) of images (see below).
- [ ] Check the `CHANGELOG.md` and update it with the new version number. Make sure the log is up to date.
  - [ ] Make sure to add any changes to already supported resources (e.g. changing labels of managed `DataPlane`s) which might cause other resources (e.g. Pods) to be recreated.
- [ ] Ensure that the `go.mod` file references an officially released version of `kong/kubernetes-configuration` instead of directly depending on the main branch version.
- [ ] Ensure that all generators have run properly (e.g. `make generate manifests`) so that updates to things like CRDs are handled for the release, double check that all manifests from `config/samples/` still work as intended.
- [ ] Ensure GitHub PAT is still valid (see [GitHub PAT](#github-pat) below).
- [ ] Ask on the `team-k8s` Slack channel for granting the CI DockerHub account temporary permission to push images to the `kong/kong-operator` repository.
- [ ] From [GitHub release action][release-action], start a new workflow run:
  - Set the `Use workflow from` to the release branch: e.g. `release/1.2.x`
    - If you want to release a major version, set the `Use workflow from` to the `main` branch otherwise set it to the release branch.
  - Set the `release` input set to the target version (e.g. `v1.2.0`).
- [ ] Wait for the workflow to complete.
- [ ] Once the workflow completes, ask for revoking the temporary permission to push images to the `kong/kong-operator` repository.
- [ ] The CI should create a PR in the [Gateway Operator][kgo-prs] repo that bumps KGO version in `VERSION` file and manifests. Merge it.
- [ ] After the PR is merged, [release-bot][release-bot-workflow] workflow will be triggered. It will create a new GH release, as well as a release branch (if not patch or prerelease):
  - [ ] Check the [releases][releases] page. The release has to be marked manually as `latest` if this is the case.
  - [ ] Check the `release/N.M.x` release branch exists.
- [ ] Update the official documentation at [github.com/Kong/docs.konghq.com][docs_repo]
  - [ ] Run post processing script for `${KUBERNETES_CONFIGURATION_REPO}/docs/gateway-operator-api-reference.md`, providing a tagged version of CRD reference from docs repo as an argument, e.g. `app/_src/gateway-operator/reference/custom-resources/1.2.x.md`.
    This will add the necessary skaffolding so that the reference is rendered correctly on docs.konghq.com.

    Example:
    ```
    ${KUBERNETES_CONFIGURATION_REPO}/scripts/apidocs-gen/post-process-for-konghq.sh ${KUBERNETES_CONFIGURATION_REPO}/docs/gateway-operator-api-reference.md ${KONGHQ_DOCS_REPO}/app/_src/gateway-operator/reference/custom-resources/1.2.x.md
    ```

**Only for major and minor releases**:

- [ ] After the release tag is created, bump the `fromVersion` in the `upgrade from one before latest to latest minor` [upgrade E2E test][helm_upgrade_test] to the one before the latest minor release.
- [ ] When the release contains breaking changes which precludes an automated upgrade make sure this is documented in the release notes and the Helm chart's [UPGRADE.md][helm-chart-upgrade].
- [ ] Schedule a retro meeting. Invite the team (team-kubernetes@konghq.com) and a Product Manager. Remember to link to [retro notes](https://docs.google.com/document/d/15gDtl425zyttbDwA8qQrh5yBgTD5OpnhjOquqfSJUx4/edit#heading=h.biunbyheelys) in the invite description


[docs_repo]: https://github.com/Kong/docs.konghq.com/
[cli_ref_docs]: https://docs.konghq.com/gateway-operator/latest/reference/cli-arguments/
[helm_upgrade_test]: https://github.com/kong/kong-operator/blob/9f33d27ab875b91e50d7e750b45a293c1395da2d/test/e2e/test_upgrade.go
[release-bot-workflow]: ../workflows/release-bot.yaml
[helm-chart-upgrade]: https://github.com/Kong/charts/blob/main/charts/gateway-operator/UPGRADE.md

## Verify default hardcoded versions

> **NOTE**: These versions should be automatically updated via Renovate.
> As part of the release workflow please verify that this is indeed the case and the automation still works.

The packages [internal/consts][consts-pkg] and [pkg/versions][versions-pkg] contains a list of default versions for the operator.
These versions should be updated to match the new release. The example consts to look for:

- `DefaultDataPlaneTag`
- `DefaultControlPlaneVersion`

## GitHub PAT

The release workflow uses @team-k8s-bot's GitHub PAT to create a GitHub release and PRs related to it.
It's named `Github team k8s bot - PAT - Kong Gateway Operator CI` in 1password and is stored in `PAT_GITHUB`
GitHub repository secret to give workflows access to it.
It's always generated with 1-year expiration date.

If you find it's expired, make sure to generate a new one and update the `PAT_GITHUB` secret as well as its 1Pass item
`Github team k8s bot - PAT - Kong Gateway Operator CI` for redundancy.

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

[releases]: https://github.com/kong/kong-operator/releases
[release-action]: https://github.com/kong/kong-operator/actions/workflows/release.yaml
[consts-pkg]: https://github.com/kong/kong-operator/blob/main/pkg/consts/consts.go
[versions-pkg]: https://github.com/kong/kong-operator/blob/main/internal/versions/
[kgo-prs]: https://github.com/kong/kong-operator/pulls
