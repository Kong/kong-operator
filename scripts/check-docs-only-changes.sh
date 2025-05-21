#!/usr/bin/env bash
set -e

# Get changed files compared to the base
CHANGED_FILES=$(git diff --name-only ${1:-$GITHUB_EVENT_PULL_REQUEST_BASE_SHA} ${2:-$GITHUB_SHA})

# Check if all changed files are documentation or license files
DOCS_ONLY=true
for file in $CHANGED_FILES; do
  if [[ ! "$file" =~ ^(CHANGELOG\.md|README\.md|SECURITY\.md|FEATURES\.md|LICENSE|LICENSES|\.github/ISSUE_TEMPLATE/.*)$ ]]; then
    DOCS_ONLY=false
    break
  fi
done

echo "docs_only=$DOCS_ONLY" >> $GITHUB_OUTPUT
echo "Changed files: $CHANGED_FILES"
echo "Docs only: $DOCS_ONLY"