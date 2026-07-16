# Changelog fragments

We generate `CHANGELOG.md` at release time from per-PR fragment files. **Do not
edit `CHANGELOG.md` directly in feature PRs.**

## How it works

1. Open a PR with a [Conventional Commits](https://www.conventionalcommits.org/)
   title, e.g. `fix(dataplane): stop false-positive env warnings`.
2. The changelog bot commits `changelog/unreleased/kong-operator/<PR-number>.yaml`
   to your branch, pre-filled from the title.
3. Edit that file if the one-line title does not capture the change. Rich,
   multi-line `message` prose is encouraged.
4. CI fails if no fragment is present. Exempt a PR with the `skip-changelog`
   label, or use a non-releasable type (`docs`, `test`, `chore`, `ci`,
   `refactor`, `style`, `build`).

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
