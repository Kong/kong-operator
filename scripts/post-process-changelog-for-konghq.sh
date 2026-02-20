#!/usr/bin/env bash
# post-process-changelog-for-konghq.sh
# Usage: ./scripts/post-process-changelog-for-konghq.sh <input-changelog> <output-file> [last-version]
#   last-version: oldest version to include (default: 1.3.0). Entries older than this are omitted.
set -euo pipefail

INPUT="${1:?input changelog file required}"
OUTPUT="${2:?output file required}"
LAST_VERSION="${3:-1.3.0}"

cat > "$OUTPUT" <<'FRONTMATTER'
---
title: "{{ site.operator_product_name }} Changelog"
description: "New features, bug fixes and breaking changes for {{ site.operator_product_name }}"
content_type: reference
layout: reference
products:
  - operator
breadcrumbs:
  - /operator/
  - index: operator
    section: Reference

---

Changelog for supported {{ site.operator_product_name }} versions.

FRONTMATTER

awk -v last_version="$LAST_VERSION" '
  # Returns 1 if semantic version a is strictly less than b (major.minor.patch).
  function version_lt(a, b,    ap, bp) {
    split(a, ap, ".")
    split(b, bp, ".")
    if (ap[1]+0 != bp[1]+0) return (ap[1]+0 < bp[1]+0)
    if (ap[2]+0 != bp[2]+0) return (ap[2]+0 < bp[2]+0)
    return (ap[3]+0 < bp[3]+0)
  }

  # Skip reference-style link definitions at the bottom (e.g. [v1.0.0]: https://...)
  /^\[v[0-9]/ { next }

  # Skip everything before the first versioned release heading
  # (covers # Changelog title, Table of Contents, Unreleased section)
  !found_release && /^## \[v[0-9]/ { found_release=1 }
  !found_release { next }

  done { next }

  # On each versioned heading, stop if the version is older than last_version
  /^## \[v[0-9]/ {
    ver = $0
    sub(/^## \[v/, "", ver)
    sub(/\].*$/, "", ver)
    if (version_lt(ver, last_version)) { done=1; next }
    # Transform ## [vX.Y.Z] -> ## X.Y.Z
    sub(/^## \[v/, "## ")
    sub(/\]$/, "")
    print; next
  }

  # Transform "> Release date: DATE" -> "**Release date**: DATE"
  /^> Release date:/ {
    sub(/^> Release date:/, "**Release date**:")
    print; next
  }

  { print }
' "$INPUT" >> "$OUTPUT"
