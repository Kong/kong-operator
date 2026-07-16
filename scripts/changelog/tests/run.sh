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

exit "$fail"
