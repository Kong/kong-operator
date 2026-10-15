#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.." # repo root
here="scripts/changelog/tests/testdata"
fail=0

check() { # name actual expected
  if diff -u "$3" "$2" >/dev/null; then
    echo "ok   - $1"
  else
    echo "FAIL - $1"; diff -u "$3" "$2" || true; fail=1
  fi
}

# --- merge test ---
tmp="$(mktemp)"
cp "$here/merge/input-changelog.md" "$tmp"
scripts/changelog/merge-changelog.sh "$tmp" "$here/merge/section.md" "v2.4.0"
check "merge-changelog" "$tmp" "$here/merge/expected-changelog.md"
rm -f "$tmp"

# --- normalize test ---
out="$(mktemp)"
scripts/changelog/normalize-section.sh "$here/normalize/raw.md" "v2.4.0" "2026-08-01" > "$out"
check "normalize-section" "$out" "$here/normalize/expected.md"
rm -f "$out"

# --- generate test (uses stub tool + isolated work dir) ---
work="$(mktemp -d)"
mkdir -p "$work/changelog/unreleased/kong-operator"
cat > "$work/CHANGELOG.md" <<'EOF'
# Changelog

## Table of Contents

- [v2.3.0](#v230)

## [v2.3.0]

> Release date: 2026-07-01

### Fixes

- Old fix.
  [#1](https://github.com/Kong/kong-operator/pull/1)
EOF
cat > "$work/changelog/unreleased/kong-operator/99.yaml" <<'EOF'
message: Stub feature entry.
type: feature
EOF
repo_root="$PWD"
(
  cd "$work"
  CHANGELOG_BIN="$repo_root/scripts/changelog/tests/stub-changelog" \
  RELEASE_DATE="2026-08-01" \
  "$repo_root/scripts/changelog/generate.sh" "v2.4.0"
)
# fragment archived
[ -f "$work/changelog/v2.4.0/99.yaml" ] && echo "ok   - generate: fragment archived" || { echo "FAIL - generate: fragment not archived"; fail=1; }
[ -z "$(ls -A "$work/changelog/unreleased/kong-operator" | grep -v '^\.gitkeep$' || true)" ] && echo "ok   - generate: unreleased drained" || { echo "FAIL - generate: unreleased not drained"; fail=1; }
# section + TOC present
grep -q '^- \[v2.4.0\](#v240)$' "$work/CHANGELOG.md" && echo "ok   - generate: toc entry" || { echo "FAIL - generate: no toc entry"; fail=1; }
grep -q '^## \[v2.4.0\]$' "$work/CHANGELOG.md" && echo "ok   - generate: section heading" || { echo "FAIL - generate: no section heading"; fail=1; }
grep -q '^> Release date: 2026-08-01$' "$work/CHANGELOG.md" && echo "ok   - generate: release date" || { echo "FAIL - generate: no release date"; fail=1; }
rm -rf "$work"

# --- generate: empty-section guard test (uses a stub that emits no entries,
# mimicking the real tool when every fragment gets skipped e.g. due to a
# wrong file extension) ---
work="$(mktemp -d)"
mkdir -p "$work/changelog/unreleased/kong-operator"
cat > "$work/CHANGELOG.md" <<'EOF'
# Changelog

## Table of Contents

- [v2.3.0](#v230)

## [v2.3.0]

> Release date: 2026-07-01

### Fixes

- Old fix.
  [#1](https://github.com/Kong/kong-operator/pull/1)
EOF
cp "$work/CHANGELOG.md" "$work/CHANGELOG.md.orig"
cat > "$work/changelog/unreleased/kong-operator/100.yaml" <<'EOF'
message: Entry the empty stub will not surface.
type: feature
EOF
repo_root="$PWD"
set +e
(
  cd "$work"
  CHANGELOG_BIN="$repo_root/scripts/changelog/tests/stub-changelog-empty" \
  RELEASE_DATE="2026-08-01" \
  "$repo_root/scripts/changelog/generate.sh" "v2.4.0"
) >"$work/generate.out" 2>"$work/generate.err"
rc=$?
set -e
if [ "$rc" -ne 0 ] && grep -q "refusing to archive fragments" "$work/generate.err"; then
  echo "ok   - generate: empty section guard fails loudly"
else
  echo "FAIL - generate: empty section guard did not fail as expected (rc=$rc)"; cat "$work/generate.err"; fail=1
fi
[ -f "$work/changelog/unreleased/kong-operator/100.yaml" ] && echo "ok   - generate: guard leaves fragment un-archived" || { echo "FAIL - generate: guard archived fragment despite empty section"; fail=1; }
[ ! -d "$work/changelog/v2.4.0" ] && echo "ok   - generate: guard creates no archive dir" || { echo "FAIL - generate: guard created archive dir despite empty section"; fail=1; }
diff -u "$work/CHANGELOG.md.orig" "$work/CHANGELOG.md" >/dev/null && echo "ok   - generate: guard leaves CHANGELOG.md untouched" || { echo "FAIL - generate: guard modified CHANGELOG.md"; fail=1; }
rm -rf "$work"

# --- real-tool extension filter test ---
# The pinned gateway-changelog binary only recognizes *.yml fragments and
# silently skips *.yaml ones (a debug-level "Skipping file" log line, no
# error, no warning at normal verbosity). The stub used above ignores file
# content/extension entirely, so it structurally cannot catch this. Exercise
# the real installed binary directly to pin this behavior down.
changelog_version=""
if command -v yq >/dev/null 2>&1; then
  changelog_version="$(yq -r '.changelog' .tools_versions.yaml 2>/dev/null | sed 's/^v//')"
fi
default_changelog_bin="$PWD/bin/installs/github-kong-gateway-changelog/${changelog_version:-unknown}/changelog"
real_bin="${CHANGELOG_BIN:-$default_changelog_bin}"

if [ -z "${GITHUB_TOKEN:-}" ]; then
  echo "skip - real-tool: extension filter (GITHUB_TOKEN not set)"
elif [ ! -x "$real_bin" ]; then
  echo "skip - real-tool: extension filter (binary not found at $real_bin; run 'make changelog-tool')"
else
  ext_dir=".changelog-realtool-test-$$"
  rm -rf "$ext_dir"
  mkdir -p "$ext_dir"

  # .yaml fragment: must be silently skipped, never reach processing/output.
  printf 'message: Realtool yaml marker should not appear.\ntype: bugfix\n' > "$ext_dir/1.yaml"
  yaml_out="$("$real_bin" --debug generate --repo-path . --changelog-paths "$ext_dir" \
    --title "vRealToolTest" --github-issue-repo Kong/kong-operator --github-api-repo Kong/kong-operator 2>&1)"
  if echo "$yaml_out" | grep -q "Realtool yaml marker"; then
    echo "FAIL - real-tool: .yaml fragment was processed (expected silent skip)"; fail=1
  elif echo "$yaml_out" | grep -q "Skipping file: 1.yaml"; then
    echo "ok   - real-tool: .yaml fragment silently skipped"
  else
    echo "FAIL - real-tool: unexpected output for .yaml fragment"; echo "$yaml_out"; fail=1
  fi
  rm -f "$ext_dir"/*.yaml

  # .yml fragment: must reach the processing path. A "no commits found" error
  # is expected and fine here (this file is untracked scratch data) -- it
  # proves the file got past the extension filter and into processing.
  printf 'message: Realtool yml marker should be processed.\ntype: bugfix\n' > "$ext_dir/1.yml"
  yml_out="$("$real_bin" --debug generate --repo-path . --changelog-paths "$ext_dir" \
    --title "vRealToolTest" --github-issue-repo Kong/kong-operator --github-api-repo Kong/kong-operator 2>&1)"
  if echo "$yml_out" | grep -q "processing changelog file: 1.yml"; then
    echo "ok   - real-tool: .yml fragment reaches processing path"
  else
    echo "FAIL - real-tool: .yml fragment did not reach processing path"; echo "$yml_out"; fail=1
  fi

  rm -rf "$ext_dir"
fi

# --- verify test ---
if scripts/changelog/verify.sh "$here/verify/good" >/dev/null 2>&1; then
  echo "ok   - verify: accepts good fragments"
else
  echo "FAIL - verify: rejected good fragments"; fail=1
fi
if scripts/changelog/verify.sh "$here/verify/bad" >/dev/null 2>&1; then
  echo "FAIL - verify: accepted bad fragment"; fail=1
else
  echo "ok   - verify: rejects bad fragments"
fi

# --- pr-fragment-status: table-driven golden tests ---
# Columns: name | PR_TITLE | PR_BODY | PR_LABELS | expected status | expected
# type | expected scope | expected author (optional, default "")
#
# Every call also asserts fragment_path against the default
# FRAGMENT_DIR/1.yml (PR_NUMBER is always 1 here) -- this is I3's fix: a
# reviewer once changed the script's `${number}.yml` to `${number}.yaml` and
# the whole suite still passed because nothing pinned fragment_path, even
# though that's the exact value the changelog-gate CI job compares against
# an on-disk file. Asserting it on every row (not just a dedicated case)
# means any future edit to fragment_path construction is caught everywhere,
# not just in one narrow test.
pfs_check() { # name title body labels expect_status expect_type expect_scope expect_author
  local name="$1" title="$2" body="$3" labels="$4" exp_status="$5" exp_type="${6:-}" exp_scope="${7:-}" author="${8:-}"
  local out status type scope fragment_path exp_fragment_path
  exp_fragment_path="changelog/unreleased/kong-operator/1.yml"
  out="$(PR_TITLE="$title" PR_BODY="$body" PR_LABELS="$labels" PR_NUMBER=1 PR_AUTHOR="$author" scripts/changelog/pr-fragment-status.sh)"
  status="$(echo "$out" | sed -n 's/^status=//p')"
  type="$(echo "$out" | sed -n 's/^type=//p')"
  scope="$(echo "$out" | sed -n 's/^scope=//p')"
  fragment_path="$(echo "$out" | sed -n 's/^fragment_path=//p')"
  if [ "$status" != "$exp_status" ]; then
    echo "FAIL - pr-fragment-status: $name (status got '$status' want '$exp_status')"; echo "$out"; fail=1
    return
  fi
  if [ "$fragment_path" != "$exp_fragment_path" ]; then
    echo "FAIL - pr-fragment-status: $name (fragment_path got '$fragment_path' want '$exp_fragment_path')"; echo "$out"; fail=1
    return
  fi
  if [ "$exp_status" = "required" ]; then
    if [ "$type" != "$exp_type" ] || [ "$scope" != "$exp_scope" ]; then
      echo "FAIL - pr-fragment-status: $name (type/scope got '$type'/'$scope' want '$exp_type'/'$exp_scope')"; echo "$out"; fail=1
      return
    fi
  fi
  echo "ok   - pr-fragment-status: $name"
}

pfs_check "deps scope on a chore title (historical drift bug)" \
  "chore(deps): bump x" "" "" required dependency deps
pfs_check "chore is exempt" \
  "chore: x" "" "" exempt
pfs_check "bang forces breaking_change" \
  "feat!: x" "" "" required breaking_change ""
pfs_check "BREAKING CHANGE in body forces breaking_change" \
  "fix: x" "BREAKING CHANGE: yes" "" required breaking_change ""
pfs_check "scoped perf maps to performance, scope kept" \
  "perf(dataplane): x" "" "" required performance dataplane
pfs_check "scope not in allowlist is blanked" \
  "feat(weirdscope): x" "" "" required feature ""
pfs_check "non-conventional title is invalid" \
  "no colon here" "" "" invalid_title
pfs_check "skip-changelog label exempts regardless of title" \
  "no colon here" "" "skip-changelog" exempt
pfs_check "multi-scope with deps still maps to dependency, scope blanked" \
  "fix(deps,ci): x" "" "" required dependency ""
pfs_check "bang on a non-releasable type still forces breaking_change" \
  "docs!: x" "" "" required breaking_change ""

# --- pr-fragment-status: P0 exemptions (bot-authored/backport PRs must not
# hard-block, but the gate must stay meaningful for humans) ---
pfs_check "backport PR title is exempt regardless of the inner commit type" \
  "[Backport release/2.2.x] fix: fix reconcile compare bugs" "" "" exempt
pfs_check "backport PR title is exempt (non-releasable inner type too)" \
  "[Backport release/2.2.x] test(envtest): stabilize config error envtest event assertions" "" "" exempt
pfs_check "cherry-pick PR title is exempt" \
  "[cherry-pick] v2.4.0 - abc123def456789" "" "" exempt
pfs_check "a human title merely mentioning backport mid-title is NOT exempt (anchored, no mid-title match)" \
  "fix: revert accidental backport regression" "" "" required bugfix ""
pfs_check "renovate[bot] with today's bare, non-Conventional title is exempt" \
  "Update module github.com/kong/go-kong to v0.78.0 (main)" "" "" exempt "" "" "renovate[bot]"
pfs_check "dependabot[bot] with a bare, non-Conventional title is exempt" \
  "Bump github.com/foo/bar from 1.0.0 to 1.0.1" "" "" exempt "" "" "dependabot[bot]"
pfs_check "any other GitHub App bot ('*[bot]') with a bare title is exempt" \
  "Automated update" "" "" exempt "" "" "github-actions[bot]"
pfs_check "a HUMAN with a non-Conventional title is still invalid_title -- the gate is not toothless" \
  "not a conventional title" "" "" invalid_title "" "" "octocat"
pfs_check "renovate[bot] with a parseable deps title (post semanticCommits:enabled) still requires a fragment" \
  "chore(deps): update module github.com/kong/go-kong to v0.78.0" "" "" required dependency deps "renovate[bot]"

# --- pr-fragment-status: SIGPIPE misclassification on a large body (I6) ---
# Reproduces the real bug: `printf '%s' "$body" | grep -q "BREAKING CHANGE"`
# under `set -o pipefail` -- grep exits as soon as it finds the match and
# closes its end of the pipe, printf gets SIGPIPE (141) because it hasn't
# finished writing a body bigger than the 64 KiB pipe buffer, and the `if`
# reads that non-zero pipeline status as "not found". GitHub's PR body limit
# (65536 chars) exceeds the pipe buffer, so this was reachable in
# production. Confirmed manually: with the old grep-pipe pattern this exact
# body classifies as bugfix; with the pure-bash `[[ == *...* ]]` fix it
# correctly classifies as breaking_change.
big_body="$(printf 'BREAKING CHANGE: this changes the wire format.\n'; head -c 70000 /dev/zero | tr '\0' 'x')"
out="$(PR_TITLE="fix: x" PR_BODY="$big_body" PR_LABELS="" PR_NUMBER=1 scripts/changelog/pr-fragment-status.sh)"
type="$(echo "$out" | sed -n 's/^type=//p')"
if [ "$type" = "breaking_change" ]; then
  echo "ok   - pr-fragment-status: BREAKING CHANGE detected in a >64KiB body (I6 SIGPIPE regression)"
else
  echo "FAIL - pr-fragment-status: BREAKING CHANGE not detected in a >64KiB body (type got '$type' want 'breaking_change')"; fail=1
fi

# --- pr-fragment-status: fragment_path honours a FRAGMENT_DIR override (I3) ---
out="$(FRAGMENT_DIR="custom/dir" PR_TITLE="fix: x" PR_BODY="" PR_LABELS="" PR_NUMBER=7 scripts/changelog/pr-fragment-status.sh)"
fragment_path="$(echo "$out" | sed -n 's/^fragment_path=//p')"
if [ "$fragment_path" = "custom/dir/7.yml" ]; then
  echo "ok   - pr-fragment-status: FRAGMENT_DIR override is reflected in fragment_path"
else
  echo "FAIL - pr-fragment-status: FRAGMENT_DIR override (fragment_path got '$fragment_path' want 'custom/dir/7.yml')"; fail=1
fi

# --- pr-fragment-status: PR_JSON_FILE mode ---
# The PR_TITLE-mode cases above never touch the jq/PR_JSON_FILE code path,
# which is what the changelog-gate CI job actually uses (via `gh api ... >
# pr.json`). Exercise that path directly, including the failure shapes a
# transient/malformed `gh api` response can produce -- the script must
# always exit 0 (policy belongs to callers) even then.
pfs_json_check() { # name json_content_or_empty_for_missing expect_status
  local name="$1" json="$2" exp_status="$3"
  local jf out rc status
  jf="$(mktemp)"
  if [ "$json" = "__MISSING__" ]; then
    rm -f "$jf"
    jf="/nonexistent-path/does-not-exist-$$.json"
  else
    printf '%s' "$json" > "$jf"
  fi
  set +e
  out="$(PR_JSON_FILE="$jf" scripts/changelog/pr-fragment-status.sh)"
  rc=$?
  set -e
  [ "$json" != "__MISSING__" ] && rm -f "$jf"
  if [ "$rc" -ne 0 ]; then
    echo "FAIL - pr-fragment-status(json): $name (expected exit 0, got $rc)"; echo "$out"; fail=1
    return
  fi
  status="$(echo "$out" | sed -n 's/^status=//p')"
  if [ "$status" != "$exp_status" ]; then
    echo "FAIL - pr-fragment-status(json): $name (status got '$status' want '$exp_status')"; echo "$out"; fail=1
    return
  fi
  echo "ok   - pr-fragment-status(json): $name"
}

pfs_json_check "valid PR JSON produces expected decision" \
  '{"title":"fix(dataplane): x","body":"","labels":[],"number":42}' required
pfs_json_check "missing PR_JSON_FILE degrades to error, not a crash" \
  "__MISSING__" error
pfs_json_check "malformed PR_JSON_FILE degrades to error, not a crash" \
  'not json at all {{{' error
pfs_json_check "bot author (.user.login) read from PR_JSON_FILE exempts a bare renovate title" \
  '{"title":"Update module github.com/kong/go-kong to v0.78.0 (main)","body":"","labels":[],"number":1,"user":{"login":"renovate[bot]"}}' exempt

# --- pr-fragment-status: locale-independence of the title regex ---
# [[:alnum:]]/[[:space:]] are locale-sensitive under bash's [[ =~ ]]; under a
# UTF-8 locale (a common GitHub Actions runner default) they'd match
# non-ASCII letters, diverging from the JS this script replaces (\w is
# always ASCII-only). Assert the ASCII-only title_re now agrees with the JS
# regardless of locale.
# Returns 2 specifically when skipped (locale unavailable), so the caller
# can tell "skip" apart from "ran and passed/failed" (see locale_exercised
# below, I8's fix) -- otherwise this whole block can go green on a machine
# that has neither locale installed, without ever re-exercising the
# regression fixed in 0a0d4afd3.
pfs_locale_check() { # name locale title expect_status
  local name="$1" loc="$2" title="$3" exp_status="$4"
  local out status
  # Scoped `set +o pipefail`: under this script's pipefail, `grep -q`
  # closing the pipe on its first match sends SIGPIPE to `locale -a`, which
  # then exits non-zero (141) even though the locale *was* found -- a false
  # "not available". The subshell keeps that relaxation local to this check.
  if ! ( set +o pipefail; locale -a 2>/dev/null | grep -qx "$loc" ); then
    echo "skip - pr-fragment-status(locale): $name (locale '$loc' not available on this machine)"
    return 2
  fi
  out="$(LC_ALL="$loc" PR_TITLE="$title" PR_BODY="" PR_LABELS="" PR_NUMBER=1 scripts/changelog/pr-fragment-status.sh)"
  status="$(echo "$out" | sed -n 's/^status=//p')"
  if [ "$status" != "$exp_status" ]; then
    echo "FAIL - pr-fragment-status(locale): $name [$loc] (status got '$status' want '$exp_status')"; echo "$out"; fail=1
    return 0
  fi
  echo "ok   - pr-fragment-status(locale): $name [$loc]"
  return 0
}

# I8: if neither locale is available anywhere on this machine, every case in
# the loop below prints "skip" and (before this fix) the suite stayed green
# with the locale-independence regression silently unexercised. Track
# whether at least one case actually ran and fail loudly if not.
locale_exercised=0
for loc in C.UTF-8 en_US.UTF-8; do
  # `|| rc=$?` (not a bare call + `$?` on the next line) so that this
  # script's own `set -e` doesn't abort the whole suite when a locale is
  # missing and the function returns 2 -- a command on the left of `||` is
  # exempt from errexit, but a bare non-zero return is not.
  rc=0
  pfs_locale_check "non-ASCII commit type matches JS (invalid_title)" "$loc" "féat: x" invalid_title || rc=$?
  [ "$rc" -ne 2 ] && locale_exercised=1
  rc=0
  pfs_locale_check "plain ASCII commit type still required" "$loc" "feat: x" required || rc=$?
  [ "$rc" -ne 2 ] && locale_exercised=1
done
if [ "$locale_exercised" -eq 0 ]; then
  echo "FAIL - pr-fragment-status(locale): neither C.UTF-8 nor en_US.UTF-8 is installed on this machine -- the locale-independence regression test (fixed in 0a0d4afd3) ran zero times this run"
  fail=1
fi

# --- skippable-paths: table-driven golden tests ---
# Drives check-docs-only-changes.sh via the CHANGED_FILES override (no git
# needed). The NOT-skippable direction is the important half: it's what
# stops a future well-meaning broadening of the regex from silently
# disabling CI for real code.
skp_check() { # name changed_files(newline-separated) expect_docs_only
  local name="$1" files="$2" exp="$3"
  local out docs_only
  out="$(CHANGED_FILES="$files" GITHUB_OUTPUT=/dev/stdout scripts/check-docs-only-changes.sh)"
  docs_only="$(echo "$out" | sed -n 's/^docs_only=//p')"
  if [ "$docs_only" != "$exp" ]; then
    echo "FAIL - skippable-paths: $name (docs_only got '$docs_only' want '$exp')"; echo "$out"; fail=1
    return
  fi
  echo "ok   - skippable-paths: $name"
}

skp_check "per-PR fragment alone is skippable" \
  "changelog/unreleased/kong-operator/4999.yml" true
skp_check "a markdown file alone is skippable" \
  "docs/some-page.md" true
skp_check "fragment + markdown mix is skippable" \
  "$(printf 'changelog/unreleased/kong-operator/4999.yml\ndocs/some-page.md')" true
skp_check "a real script is not skippable" \
  "scripts/changelog/verify.sh" false
skp_check "the fragment-scaffolding workflow itself is not skippable" \
  ".github/workflows/changelog-fragment.yaml" false
skp_check "the schema template doc is not skippable" \
  "changelog/unreleased/kong-operator/CHANGELOG_TEMPLATE.yaml" false
skp_check "wrong extension (.yaml instead of .yml) is not skippable" \
  "changelog/unreleased/kong-operator/4999.yaml" false
skp_check "a Go file is not skippable" \
  "controller/dataplane/controller.go" false
skp_check "one non-skippable file poisons a fragment mix" \
  "$(printf 'changelog/unreleased/kong-operator/4999.yml\ncontroller/dataplane/controller.go')" false

exit "$fail"
