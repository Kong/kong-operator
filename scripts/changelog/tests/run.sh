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

exit "$fail"
