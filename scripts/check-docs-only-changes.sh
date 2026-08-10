#!/usr/bin/env bash
set -e

# Get changed files compared to the base. CHANGED_FILES can be pre-set in the
# environment (used by the test suite to drive this classifier without git);
# otherwise it's computed from git diff as before.
CHANGED_FILES="${CHANGED_FILES:-$(git diff --name-only ${1:-$GITHUB_EVENT_PULL_REQUEST_BASE_SHA} ${2:-$GITHUB_SHA})}"

# Check if all changed files are ones that cannot change a test outcome:
# docs/license/template files, or a per-PR changelog fragment (schema-linted
# separately, never executed). "docs_only" below means exactly that -- not
# literally "only documentation" -- so a fragment-only PR is also docs_only.
DOCS_ONLY=true
for file in $CHANGED_FILES; do
  if [[ ! "$file" =~ ^(LICENSE|LICENSES|CODEOWNERS|\.github/ISSUE_TEMPLATE/.*|changelog/unreleased/[^/]+/[0-9]+\.yml|.*\.md)$ ]]; then
    DOCS_ONLY=false
    break
  fi
done

echo "docs_only=$DOCS_ONLY" >> $GITHUB_OUTPUT
echo "Changed files: $CHANGED_FILES"
echo "No-test-impact only (docs/license/fragment): $DOCS_ONLY"
