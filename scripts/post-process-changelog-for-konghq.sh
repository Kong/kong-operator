#!/usr/bin/env bash
# post-process-changelog-for-konghq.sh
# Usage: ./scripts/post-process-changelog-for-konghq.sh <input-changelog> <output-file>
set -euo pipefail

INPUT="${1:?input changelog file required}"
OUTPUT="${2:?output file required}"

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

---

Changelog for supported {{ site.operator_product_name }} versions.

FRONTMATTER

awk '
  # Skip reference-style link definitions at the bottom (e.g. [v1.0.0]: https://...)
  /^\[v[0-9]/ { next }

  # Skip everything before the first versioned release heading
  # (covers # Changelog title, Table of Contents, Unreleased section)
  !found_release && /^## \[v[0-9]/ { found_release=1 }
  !found_release { next }

  # Transform ## [vX.Y.Z] -> ## X.Y.Z
  /^## \[v[0-9]/ {
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
