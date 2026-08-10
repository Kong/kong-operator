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
# Columns: name | PR_TITLE | PR_BODY | PR_LABELS | expected status | expected type | expected scope
pfs_check() { # name title body labels expect_status expect_type expect_scope
  local name="$1" title="$2" body="$3" labels="$4" exp_status="$5" exp_type="${6:-}" exp_scope="${7:-}"
  local out status type scope
  out="$(PR_TITLE="$title" PR_BODY="$body" PR_LABELS="$labels" PR_NUMBER=1 scripts/changelog/pr-fragment-status.sh)"
  status="$(echo "$out" | sed -n 's/^status=//p')"
  type="$(echo "$out" | sed -n 's/^type=//p')"
  scope="$(echo "$out" | sed -n 's/^scope=//p')"
  if [ "$status" != "$exp_status" ]; then
    echo "FAIL - pr-fragment-status: $name (status got '$status' want '$exp_status')"; echo "$out"; fail=1
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

exit "$fail"
