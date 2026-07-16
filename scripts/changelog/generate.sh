#!/usr/bin/env bash
# Assemble the changelog section for a release from unreleased fragments,
# prepend it into CHANGELOG.md, and archive consumed fragments.
# Usage: generate.sh <version>
# Env: CHANGELOG_BIN (path to gateway-changelog binary, default `changelog`),
#      REPO (default Kong/kong-operator), RELEASE_DATE (default today UTC).
set -euo pipefail

version="${1:?usage: generate.sh <version>}"
here="$(cd "$(dirname "$0")" && pwd)"
frag_dir="changelog/unreleased/kong-operator"
archive_dir="changelog/${version}"
changelog="CHANGELOG.md"
bin="${CHANGELOG_BIN:-changelog}"
repo="${REPO:-Kong/kong-operator}"
release_date="${RELEASE_DATE:-$(date -u +%Y-%m-%d)}"

shopt -s nullglob
frags=("$frag_dir"/*.yaml "$frag_dir"/*.yml)
# Exclude the template from the fragment set.
# Note: array expansions are guarded by a length check because bash 3.2
# (macOS's default /bin/bash) treats "${arr[@]}" on an empty array as an
# unbound-variable error under `set -u`.
tmp_frags=()
if [ "${#frags[@]}" -gt 0 ]; then
  for f in "${frags[@]}"; do
    case "$(basename "$f")" in CHANGELOG_TEMPLATE.yaml) ;; *) tmp_frags+=("$f") ;; esac
  done
fi
frags=()
if [ "${#tmp_frags[@]}" -gt 0 ]; then
  frags=("${tmp_frags[@]}")
fi

if [ "${#frags[@]}" -eq 0 ]; then
  echo "No changelog fragments in ${frag_dir}; nothing to generate."
  exit 0
fi

raw="$(mktemp)"
"$bin" generate \
  --repo-path . \
  --changelog-paths "$frag_dir" \
  --title "$version" \
  --github-issue-repo "$repo" \
  --github-api-repo "$repo" \
  > "$raw"

section="$(mktemp)"
"$here/normalize-section.sh" "$raw" "$version" "$release_date" > "$section"
"$here/merge-changelog.sh" "$changelog" "$section" "$version"
rm -f "$raw" "$section"

mkdir -p "$archive_dir"
for f in "${frags[@]}"; do
  git mv "$f" "$archive_dir/" 2>/dev/null || mv "$f" "$archive_dir/"
done

echo "Generated ${version} section from ${#frags[@]} fragment(s); archived to ${archive_dir}."
