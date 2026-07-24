#!/usr/bin/env bash
# Prepend a version section + TOC entry into CHANGELOG.md, in place.
# Usage: merge-changelog.sh <changelog-file> <section-file> <version>
set -euo pipefail

changelog="${1:?changelog file}"
section="${2:?section file}"
version="${3:?version}"

# GitHub heading anchor: lowercase, drop chars other than [a-z0-9 _-], spaces->'-'
anchor="$(printf '%s' "$version" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9 _-]//g' | tr ' ' '-')"
toc_line="- [${version}](#${anchor})"

tmp="$(mktemp)"
awk -v toc="$toc_line" -v secfile="$section" '
  BEGIN { toc_done=0; sec_done=0 }
  /^## Table of Contents$/ && !toc_done {
    print               # heading
    getline; print      # blank line after heading
    print toc           # new entry, above existing list
    toc_done=1
    next
  }
  /^## \[/ && !sec_done {
    while ((getline line < secfile) > 0) print line
    print ""
    sec_done=1
  }
  { print }
' "$changelog" > "$tmp"
mv "$tmp" "$changelog"
