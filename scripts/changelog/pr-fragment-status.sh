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
#                  An unreadable file or invalid JSON is reported as
#                  status=error (see below), not a non-zero exit -- a
#                  transient `gh api` hiccup must not abort the caller.
#   PR_TITLE       PR title. Used directly when PR_JSON_FILE is unset.
#   PR_BODY        PR body (default "").
#   PR_LABELS      Newline-separated label names (default "").
#   PR_NUMBER      PR number (default "").
#   FRAGMENT_DIR   Fragment directory (default changelog/unreleased/kong-operator).
#
# Outputs: `key=value` lines on stdout, and appended to $GITHUB_OUTPUT when
# that env var is set (multiline-safe: a value containing a newline is
# written with the `key<<DELIM` heredoc form instead of `key=value`).
#   status         required | exempt | invalid_title | error
#   reason         human string, safe to reuse verbatim in logs/errors
#   type           only when status=required: feature|bugfix|performance|
#                  dependency|breaking_change
#   scope          only when status=required (may be empty)
#   message        only when status=required: the commit subject
#   fragment_path  <FRAGMENT_DIR>/<PR_NUMBER>.yml (empty if PR_NUMBER unset,
#                  including for status=error, which never resolves a
#                  PR number)
set -euo pipefail

fragment_dir="${FRAGMENT_DIR:-changelog/unreleased/kong-operator}"

emit() { # key value
  local key="$1" value="$2"
  printf '%s=%s\n' "$key" "$value"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    if [[ "$value" == *$'\n'* ]]; then
      # Multiline-safe form. Every value emitted today is single-line (PR
      # titles can't contain newlines), but this makes that an enforced
      # invariant rather than a silent assumption a future edit could break.
      local delim="EOF_${RANDOM}_${RANDOM}"
      {
        printf '%s<<%s\n' "$key" "$delim"
        printf '%s\n' "$value"
        printf '%s\n' "$delim"
      } >> "$GITHUB_OUTPUT"
    else
      printf '%s=%s\n' "$key" "$value" >> "$GITHUB_OUTPUT"
    fi
  fi
}

if [ -n "${PR_JSON_FILE:-}" ]; then
  if [ ! -r "${PR_JSON_FILE}" ]; then
    emit status error
    emit reason "PR_JSON_FILE '${PR_JSON_FILE}' does not exist or is not readable"
    emit fragment_path ""
    exit 0
  fi
  # Validate + parse in one step so a malformed PR_JSON_FILE degrades to
  # status=error instead of aborting the script under `set -e` (jq's
  # non-zero exit on a parse error would otherwise kill the whole process
  # mid-run, e.g. because of a transient/malformed `gh api` response).
  if ! pr_json="$(jq -e -c '.' "${PR_JSON_FILE}" 2>/dev/null)"; then
    emit status error
    emit reason "PR_JSON_FILE '${PR_JSON_FILE}' is not valid JSON"
    emit fragment_path ""
    exit 0
  fi
  title="$(jq -r '.title // ""' <<< "$pr_json")"
  body="$(jq -r '.body // ""' <<< "$pr_json")"
  labels="$(jq -r '.labels[]?.name // empty' <<< "$pr_json")"
  number="$(jq -r '.number // "" | tostring' <<< "$pr_json")"
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
#
# Deliberately NOT [[:alnum:]_] / [[:space:]]: those POSIX classes are
# locale-sensitive under bash's [[ =~ ]] (glibc regex), so under a UTF-8
# locale (e.g. C.UTF-8 or en_US.UTF-8 -- both common GitHub Actions runner
# defaults) [[:alnum:]] matches non-ASCII letters too, e.g. a title starting
# "féat:" would match and get classified exempt/required instead of
# invalid_title. JS's \w is always ASCII-only regardless of locale/flags, so
# matching it exactly means hard-coding ASCII ranges here rather than
# depending on the runtime locale. The whitespace class is spelled out as
# literal ASCII space/tab/newline/CR/FF/VT (via $'...' quoting) for the same
# reason, rather than [[:space:]].
#
# A script-wide `export LC_ALL=C` was considered instead (simpler: one line
# up top instead of a hand-spelled class here) but rejected: it would also
# silently change trim()'s [[:space:]] and any future [[ =~ ]]/glob added to
# this file, which is exactly the kind of implicit, easy-to-forget behaviour
# this fix is trying to eliminate. Pinning the one regex that must match the
# JS is more surgical and self-documenting at the point of use.
title_re=$'^([A-Za-z0-9_]+)(\\(([^)]*)\\))?(!)?:[ \t\n\r\f\v]*(.+)$'
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
