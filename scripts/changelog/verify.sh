#!/usr/bin/env bash
# Schema-lint changelog fragments in a directory.
# Usage: verify.sh [dir]   (default: changelog/unreleased/kong-operator)
set -euo pipefail

dir="${1:-changelog/unreleased/kong-operator}"
allowed="feature bugfix dependency deprecation breaking_change performance"
rc=0

shopt -s nullglob
for f in "$dir"/*.yaml "$dir"/*.yml; do
  case "$(basename "$f")" in CHANGELOG_TEMPLATE.yaml) continue ;; esac

  msg="$(yq -r '.message // ""' "$f")"
  if [ -z "$msg" ] || [ "$msg" = "null" ]; then
    echo "ERROR: $f: missing required 'message'"; rc=1
  fi

  typ="$(yq -r '.type // ""' "$f")"
  if [ -z "$typ" ] || [ "$typ" = "null" ]; then
    echo "ERROR: $f: missing required 'type'"; rc=1
  else
    # Intentional word-splitting over $allowed (a space-separated list),
    # same idiom as pr-fragment-status.sh's allowed_scopes/skip_types loops:
    # iterate the words to find an exact match rather than piping through
    # printf/grep, which sidesteps the SC2086 quoting question entirely.
    type_ok=""
    for a in $allowed; do
      [ "$a" = "$typ" ] && type_ok=1
    done
    if [ -z "$type_ok" ]; then
      echo "ERROR: $f: invalid type '$typ' (allowed: $allowed)"; rc=1
    fi
  fi
done

exit "$rc"
