# Changelog fragments

We generate `CHANGELOG.md` at release time from per-PR fragment files. **Do not
edit `CHANGELOG.md` directly in feature PRs.**

## How it works

1. Open a PR with a [Conventional Commits](https://www.conventionalcommits.org/)
   title, e.g. `fix(dataplane): stop false-positive env warnings`.
2. The changelog bot commits `changelog/unreleased/kong-operator/<PR-number>.yml`
   to your branch, pre-filled from the title.
3. Edit that file if the one-line title does not capture the change. Rich,
   multi-line `message` prose is encouraged.
4. CI fails if no fragment is present. Exempt a PR with the `skip-changelog`
   label, or use a non-releasable type (`docs`, `test`, `chore`, `ci`,
   `refactor`, `style`, `build`).
5. The fragment must be named `<PR-number>.yml` (a `.yaml` sibling is not
   recognized by the generator or the CI gate — `changelog-gate` still
   schema-lints any `.yaml` file it finds, so a malformed one still fails
   CI, but the release-time generator silently skips it, so a `.yaml`
   fragment never actually makes it into `CHANGELOG.md`).
6. A PR that only touches its own changelog fragment skips the expensive
   test suite (kind/e2e jobs) entirely — the `changelog-gate` job still
   schema-lints the fragment, so a malformed fragment (e.g. an invalid
   `type`) still fails CI even on a fragment-only PR.
7. `changelog-gate` reads the PR's live title/body/labels, not the payload
   from when the workflow was triggered. So after fixing the title or adding
   the `skip-changelog` label, no new commit is needed: someone with write
   access can click "Re-run all jobs" on the `tests` workflow once and the
   gate re-evaluates against the current PR state.
8. Backport/cherry-pick PRs (title starting `[Backport ...]` or
   `[cherry-pick]`) and PRs authored by a known bot (`renovate[bot]`,
   `dependabot[bot]`, or any `*[bot]` GitHub App) are exempt — a
   backport/cherry-pick PR's fragment travels with the original PR's
   cherry-picked commit, so a second one would just duplicate the entry at
   release time; bot PRs get the exemption as a safety net for titles that
   don't parse as a Conventional Commit, since there's no human on the
   other end to fix an unmergeable title. A bot PR whose title *does* parse
   (e.g. a Renovate `chore(deps): ...` title) is classified normally and
   still needs a fragment like any other PR.

## Fragment schema

See `unreleased/kong-operator/CHANGELOG_TEMPLATE.yaml`.

- `message` (required): description, may be multi-line.
- `type` (required): `feature`, `bugfix`, `dependency`, `deprecation`,
  `breaking_change`, `performance`.
- `scope` (optional): `dataplane`, `controlplane`, `gateway`, `hybridgateway`,
  `konnect`, `aigateway`, `eventgateway`, `crd`, `deps`.

## Conventional-commit → type mapping

| PR title prefix | fragment `type` |
|---|---|
| `feat` | `feature` |
| `fix` | `bugfix` |
| `perf` | `performance` |
| any `!` / body `BREAKING CHANGE` | `breaking_change` |
| `deps` scope, renovate/dependabot | `dependency` |
| `docs`,`test`,`chore`,`ci`,`refactor`,`style`,`build` | no fragment |

## Generating (release time only)

`make changelog VERSION=vX.Y.Z` — assembles the version section into
`CHANGELOG.md` and moves consumed fragments to `changelog/<version>/`.
The release workflow runs this automatically.
