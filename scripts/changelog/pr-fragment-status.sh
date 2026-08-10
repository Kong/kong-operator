#!/usr/bin/env bash
# Decide whether a PR needs a changelog fragment, based on its Conventional
# Commit title (plus body/labels overrides). Pure function: no git, no `gh`,
# no filesystem access beyond an optional PR_JSON_FILE. Always exits 0 --
# policy (failing a check, etc.) belongs to callers.
#
# Ports (verbatim in behaviour) the JS that used to be duplicated across two
# inline `actions/github-script` blocks in
# .github/workflows/changelog-fragment.yaml.
#
# Inputs (env only):
#   PR_JSON_FILE   Path to a GitHub PR JSON object (.title, .body,
#                  .labels[].name, .number). Used by CI, since it avoids
#                  passing a multiline PR body through a shell env var.
#                  If set, PR_TITLE/PR_BODY/PR_LABELS/PR_NUMBER are ignored.
#   PR_TITLE       PR title. Used directly when PR_JSON_FILE is unset.
#   PR_BODY        PR body (default "").
#   PR_LABELS      Newline-separated label names (default "").
#   PR_NUMBER      PR number (default "").
#   FRAGMENT_DIR   Fragment directory (default changelog/unreleased/kong-operator).
#
# Outputs: `key=value` lines on stdout, and appended to $GITHUB_OUTPUT when
# that env var is set.
#   status         required | exempt | invalid_title
#   reason         human string, safe to reuse verbatim in logs/errors
#   type           only when status=required: feature|bugfix|performance|
#                  dependency|breaking_change
#   scope          only when status=required (may be empty)
#   message        only when status=required: the commit subject
#   fragment_path  <FRAGMENT_DIR>/<PR_NUMBER>.yml (empty if PR_NUMBER unset)
set -euo pipefail

fragment_dir="${FRAGMENT_DIR:-changelog/unreleased/kong-operator}"

if [ -n "${PR_JSON_FILE:-}" ]; then
  title="$(jq -r '.title // ""' "$PR_JSON_FILE")"
  body="$(jq -r '.body // ""' "$PR_JSON_FILE")"
  labels="$(jq -r '.labels[]?.name // empty' "$PR_JSON_FILE")"
  number="$(jq -r '.number // "" | tostring' "$PR_JSON_FILE")"
else
  title="${PR_TITLE:-}"
  body="${PR_BODY:-}"
  labels="${PR_LABELS:-}"
  number="${PR_NUMBER:-}"
fi

fragment_path=""
if [ -n "$number" ]; then
  fragment_path="${fragment_dir}/${number}.yml"
fi

emit() { # key value
  printf '%s=%s\n' "$1" "$2"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s=%s\n' "$1" "$2" >> "$GITHUB_OUTPUT"
  fi
}

trim() {
  local s="$1"
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s' "$s"
}

# skip-changelog label always wins, regardless of title.
while IFS= read -r label; do
  if [ "$label" = "skip-changelog" ]; then
    emit status exempt
    emit reason "skip-changelog label present"
    emit fragment_path "$fragment_path"
    exit 0
  fi
done <<< "$labels"

# JS: /^(\w+)(?:\(([^)]*)\))?(!)?:\s*(.+)$/
# ERE translation: the non-capturing (?:...) becomes capturing, shifting
# indices -- group 2 is "(scope)" (with parens), group 3 is the scope
# content, group 4 is the "!" bang, group 5 is the subject.
title_re='^([[:alnum:]_]+)(\(([^)]*)\))?(!)?:[[:space:]]*(.+)$'
if [[ ! "$title" =~ $title_re ]]; then
  emit status invalid_title
  emit reason "PR title is not a Conventional Commit (e.g. 'fix(dataplane): ...'). Fix the title or add the 'skip-changelog' label."
  emit fragment_path "$fragment_path"
  exit 0
fi

cc_type="${BASH_REMATCH[1]}"
cc_scope="${BASH_REMATCH[3]}"
bang="${BASH_REMATCH[4]}"
subject="${BASH_REMATCH[5]}"

type=""
case "$cc_type" in
  feat) type="feature" ;;
  fix) type="bugfix" ;;
  perf) type="performance" ;;
esac

# deps scope (comma-separated, trimmed) overrides the mapped type.
if [ -n "$cc_scope" ]; then
  scope_parts=()
  IFS=',' read -ra scope_parts <<< "$cc_scope"
  for part in "${scope_parts[@]}"; do
    if [ "$(trim "$part")" = "deps" ]; then
      type="dependency"
    fi
  done
fi

# "!" or a "BREAKING CHANGE" body marker overrides everything else.
if [ -n "$bang" ] || printf '%s' "$body" | grep -q "BREAKING CHANGE"; then
  type="breaking_change"
fi

if [ -z "$type" ]; then
  skip_types="docs test chore ci refactor style build"
  is_skip=""
  for s in $skip_types; do
    [ "$cc_type" = "$s" ] && is_skip=1
  done
  emit status exempt
  if [ -n "$is_skip" ]; then
    emit reason "non-releasable commit type '$cc_type'"
  else
    emit reason "unmapped commit type '$cc_type'"
  fi
  emit fragment_path "$fragment_path"
  exit 0
fi

allowed_scopes="dataplane controlplane gateway hybridgateway konnect aigateway eventgateway crd deps"
scope=""
for s in $allowed_scopes; do
  [ "$cc_scope" = "$s" ] && scope="$cc_scope"
done

emit status required
emit reason "conventional commit type '$cc_type' requires a changelog fragment (type=$type)"
emit type "$type"
emit scope "$scope"
emit message "$subject"
emit fragment_path "$fragment_path"
