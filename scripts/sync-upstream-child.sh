#!/usr/bin/env bash
# Synchronize the upstream mainline and selectively port child commits by state and safety rules.
# Used by GitHub Actions and for manual verification in a clean local clone.

set -euo pipefail

# Color definitions
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

UPSTREAM_URL="${UPSTREAM_URL:-https://github.com/chenyme/grok2api.git}"
CHILD_URL="${CHILD_URL:-https://github.com/lij768423-svg/grok2api.git}"
UPSTREAM_REF="${UPSTREAM_REF:-main}"
CHILD_REF="${CHILD_REF:-main}"
SYNC_BRANCH="${SYNC_BRANCH:-automation/upstream-child-sync}"
STATE_FILE="${STATE_FILE:-.github/child-sync-state.json}"
REPORT_FILE="${SYNC_REPORT_FILE:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/grok2api-sync-report.md}"

declare -a APPLIED=()
declare -a EXCLUDED=()
declare -a PENDING=()
BLOCKED=false

# Log helper
log() {
    local color=$1
    local message=$2
    printf '%b[%s] %s%b\n' "$color" "$(date '+%Y-%m-%d %H:%M:%S')" "$message" "$NC"
}

# Fatal error helper
die() {
    log "$RED" "error: $*"
    exit 1
}

# Ensure a remote points to the expected repository.
ensure_remote() {
    local name=$1
    local url=$2
    if git remote get-url "$name" >/dev/null 2>&1; then
        git remote set-url "$name" "$url"
    else
        git remote add "$name" "$url"
    fi
}

# Check whether the state file already records a commit.
state_has() {
    local bucket=$1
    local commit=$2
    jq -e --arg bucket "$bucket" --arg commit "$commit" \
        '((.[$bucket] // []) | map(select(.commit == $commit)) | length) > 0' \
        "$STATE_FILE" >/dev/null
}

# Add an integrated entry and clear a pending entry for the same commit.
state_add_integrated() {
    local commit=$1
    local mode=$2
    local note=$3
    local temp_file
    temp_file=$(mktemp "${STATE_FILE}.XXXXXX")
    jq --arg commit "$commit" --arg mode "$mode" --arg note "$note" '
        .integrated = ((.integrated // [])
            | map(select(.commit != $commit))
            + [{"commit": $commit, "mode": $mode, "note": $note}])
        | .pending = ((.pending // []) | map(select(.commit != $commit)))
    ' "$STATE_FILE" > "$temp_file"
    mv "$temp_file" "$STATE_FILE"
}

# Record documentation or release commits that need no automatic port.
state_add_excluded() {
    local commit=$1
    local reason=$2
    local temp_file
    temp_file=$(mktemp "${STATE_FILE}.XXXXXX")
    jq --arg commit "$commit" --arg reason "$reason" '
        .excluded = ((.excluded // [])
            | map(select(.commit != $commit))
            + [{"commit": $commit, "reason": $reason}])
        | .pending = ((.pending // []) | map(select(.commit != $commit)))
    ' "$STATE_FILE" > "$temp_file"
    mv "$temp_file" "$STATE_FILE"
}

# Record a commit for manual review; stop so dependent commits are not skipped.
state_add_pending() {
    local commit=$1
    local reason=$2
    local temp_file
    temp_file=$(mktemp "${STATE_FILE}.XXXXXX")
    jq --arg commit "$commit" --arg reason "$reason" '
        .pending = ((.pending // [])
            | map(select(.commit != $commit))
            + [{"commit": $commit, "reason": $reason}])
    ' "$STATE_FILE" > "$temp_file"
    mv "$temp_file" "$STATE_FILE"
}

# Check whether a commit only changes documentation, versions, or release metadata.
is_nonfunctional_commit() {
    local commit=$1
    local file
    local count=0
    while IFS= read -r file; do
        [[ -n "$file" ]] || continue
        count=$((count + 1))
        case "$file" in
            README*|AI_*.md|VERSION|.github/*|docker-compose.yml|Dockerfile|config.example.yaml|tools/egress-quality-guard/README*)
                ;;
            *)
                return 1
                ;;
        esac
    done < <(git diff-tree --root --no-commit-id --name-only -r "$commit")
    ((count > 0))
}

# Only allow small, non-deleting code commits into the sync branch automatically.
is_safe_code_commit() {
    local commit=$1
    local file
    local count=0
    local changed_lines

    if [[ -n "$(git diff-tree --root --no-commit-id --name-only --diff-filter=D -r "$commit")" ]]; then
        return 1
    fi

    while IFS= read -r file; do
        [[ -n "$file" ]] || continue
        count=$((count + 1))
        case "$file" in
            backend/*|frontend/*|scripts/*.py|tools/egress-quality-guard/*.py)
                ;;
            *)
                return 1
                ;;
        esac
    done < <(git diff-tree --root --no-commit-id --name-only -r "$commit")

    ((count > 0 && count <= 20)) || return 1
    changed_lines=$(git show --format= --numstat "$commit" \
        | awk '$1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ { total += $1 + $2 } END { print total + 0 }')
    ((changed_lines <= 800))
}

# Write the report used by the PR and the Actions summary.
write_report() {
    mkdir -p "$(dirname "$REPORT_FILE")"
    {
        printf '# Upstream and child sync report\n\n'
        printf -- '- Upstream repository: `%s`\n' "$UPSTREAM_URL"
        printf -- '- Upstream baseline: `%s`\n' "$(git rev-parse "refs/remotes/upstream/$UPSTREAM_REF")"
        printf -- '- Child repository: `%s`\n' "$CHILD_URL"
        printf -- '- Child baseline: `%s`\n' "$(git rev-parse "refs/remotes/child/$CHILD_REF")"
        printf -- '- Sync branch: `%s`\n\n' "$SYNC_BRANCH"
        printf '## Applied\n\n'
        if ((${#APPLIED[@]} == 0)); then
            printf -- '- No new commits were automatically ported in this run.\n'
        else
            printf '%s\n' "${APPLIED[@]}"
        fi
        printf '\n## Excluded\n\n'
        if ((${#EXCLUDED[@]} == 0)); then
            printf -- '- No new commits were excluded in this run.\n'
        else
            printf '%s\n' "${EXCLUDED[@]}"
        fi
        printf '\n## Pending manual review\n\n'
        if ((${#PENDING[@]} == 0)); then
            printf -- '- No blocking items.\n'
        else
            printf '%s\n' "${PENDING[@]}"
        fi
        printf '\n> Upstream changes are merged first; child commits are ported selectively by the state file and safety rules. Review this PR before merging it into `main`.\n'
    } > "$REPORT_FILE"

    if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
        cat "$REPORT_FILE" >> "$GITHUB_STEP_SUMMARY"
    fi
}

main() {
    command -v git >/dev/null 2>&1 || die "git is required"
    command -v jq >/dev/null 2>&1 || die "jq is required"
    [[ -f "$STATE_FILE" ]] || die "state file is missing: $STATE_FILE"
    jq -e . "$STATE_FILE" >/dev/null || die "state file is not valid JSON: $STATE_FILE"

    [[ -z "$(git status --porcelain)" ]] || die "the worktree must be clean before synchronization"
    git config user.name "ganzizi-sync[bot]"
    git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
    ensure_remote upstream "$UPSTREAM_URL"
    ensure_remote child "$CHILD_URL"

    log "$BLUE" "fetching upstream, child, and the existing sync branch"
    git fetch --no-tags --prune upstream "refs/heads/$UPSTREAM_REF:refs/remotes/upstream/$UPSTREAM_REF"
    git fetch --no-tags --prune child "refs/heads/$CHILD_REF:refs/remotes/child/$CHILD_REF"
    git fetch --no-tags --prune origin "refs/heads/main:refs/remotes/origin/main"

    local remote_sync_exists=false
    if git ls-remote --exit-code origin "refs/heads/$SYNC_BRANCH" >/dev/null 2>&1; then
        git fetch --no-tags origin "refs/heads/$SYNC_BRANCH:refs/remotes/origin/$SYNC_BRANCH"
        remote_sync_exists=true
    fi

    if [[ "$remote_sync_exists" == true ]]; then
        git switch --detach "refs/remotes/origin/$SYNC_BRANCH"
    else
        git switch --detach "refs/remotes/origin/main"
    fi

    local before_official
    before_official=$(git rev-parse HEAD)
    if ! git merge --no-edit "refs/remotes/origin/main" >/dev/null; then
        git merge --abort || true
        write_report
        die "the sync branch conflicts with this fork's main; resolve it manually first"
    fi
    if ! git merge --no-edit "refs/remotes/upstream/$UPSTREAM_REF" >/dev/null; then
        git merge --abort || true
        write_report
        die "upstream changes conflict with the sync branch; resolve them manually first"
    fi
    local after_official
    after_official=$(git rev-parse HEAD)
    if [[ "$before_official" != "$after_official" ]]; then
        log "$GREEN" "merged upstream changes into the sync branch"
    fi

    local candidate
    candidate=$(git rev-parse HEAD)
    mapfile -t child_commits < <(
        git log --reverse --cherry-pick --right-only --no-merges --format='%H' \
            "$candidate...refs/remotes/child/$CHILD_REF"
    )

    local commit subject files reason
    for commit in "${child_commits[@]}"; do
        if state_has integrated "$commit" || state_has excluded "$commit"; then
            continue
        fi
        if state_has pending "$commit"; then
            subject=$(git show -s --format='%s' "$commit")
            PENDING+=("- \`$commit\` $subject: the state file marks this commit for review")
            BLOCKED=true
            break
        fi

        subject=$(git show -s --format='%s' "$commit")
        files=$(git diff-tree --root --no-commit-id --name-only -r "$commit" | paste -sd ', ' -)
        if is_nonfunctional_commit "$commit"; then
            reason="documentation, version, or release metadata only; no automatic behavior change"
            state_add_excluded "$commit" "$reason"
            EXCLUDED+=("- \`$commit\` $subject: $reason")
            continue
        fi
        if ! is_safe_code_commit "$commit"; then
            reason="the change includes sensitive paths, deletions, or exceeds the automatic review size (files: $files)"
            state_add_pending "$commit" "$reason"
            PENDING+=("- \`$commit\` $subject: $reason")
            BLOCKED=true
            break
        fi

        log "$CYAN" "porting child commit $commit $subject"
        if git cherry-pick -x "$commit"; then
            state_add_integrated "$commit" "auto" "code paths match the sync safety rules; cherry-picked automatically"
            APPLIED+=("- \`$commit\` $subject")
            continue
        fi

        local cherry_head
        cherry_head=$(git rev-parse --git-path CHERRY_PICK_HEAD)
        if [[ -f "$cherry_head" ]] && [[ -z "$(git diff --name-only --diff-filter=U)" ]]; then
            git cherry-pick --skip
            state_add_integrated "$commit" "equivalent" "the cherry-pick was empty; the sync branch already has equivalent content"
            APPLIED+=("- \`$commit\` $subject (equivalent content already present)")
            continue
        fi

        git cherry-pick --abort || true
        reason="the cherry-pick conflicted; re-port it manually against the upstream version"
        state_add_pending "$commit" "$reason"
        PENDING+=("- \`$commit\` $subject: $reason")
        BLOCKED=true
        break
    done

    if ! git diff --quiet -- "$STATE_FILE"; then
        git add "$STATE_FILE"
        git commit -m "chore: record upstream and child sync state"
    fi

    write_report
    local has_changes=false
    if ! git diff --quiet "refs/remotes/origin/main" HEAD; then
        has_changes=true
    fi

    if [[ "$has_changes" == true ]]; then
        log "$BLUE" "pushing sync branch $SYNC_BRANCH (fast-forward only; no force push)"
        git push origin "HEAD:refs/heads/$SYNC_BRANCH"
    else
        log "$GREEN" "no file changes to push"
    fi

    if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
        printf 'sync_branch=%s\n' "$SYNC_BRANCH" >> "$GITHUB_OUTPUT"
        printf 'has_changes=%s\n' "$has_changes" >> "$GITHUB_OUTPUT"
        printf 'report_file=%s\n' "$REPORT_FILE" >> "$GITHUB_OUTPUT"
        printf 'blocked=%s\n' "$BLOCKED" >> "$GITHUB_OUTPUT"
    fi

    if [[ "$BLOCKED" == true ]]; then
        log "$YELLOW" "sync stopped at a pending commit; upstream changes and the state report remain on the sync branch"
    fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
