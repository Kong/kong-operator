#!/usr/bin/env bash
# Normalize gateway-changelog output to the repo's canonical section format.
# Usage: normalize-section.sh <raw-file> <version> <date>  (prints to stdout)
set -euo pipefail

raw="${1:?raw file}"
version="${2:?version}"
date="${3:?date}"

awk -v ver="## [${version}]" -v dline="> Release date: ${date}" '
  BEGIN { started=0; afterheading=0 }
  !started && $0 ~ /^[[:space:]]*$/ { next }          # skip leading blank lines
  !started {
    started=1
    print ver; print ""; print dline; print ""
    if ($0 ~ /^#/) { afterheading=1; next }            # drop tool heading line
    print                                              # non-heading first line: keep body
    next
  }
  afterheading && $0 ~ /^[[:space:]]*$/ { next }        # skip blank line(s) right after dropped heading
  { afterheading=0; print }
' "$raw"
