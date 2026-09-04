#!/usr/bin/env bash
# 同步官方主线，并按状态清单和安全规则选择性移植子分支提交。
# 用途：由 GitHub Actions 定时运行，也可在干净的本地克隆中手动验证。

set -euo pipefail

# 颜色定义
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

# 日志函数
log() {
    local color=$1
    local message=$2
    printf '%b[%s] %s%b\n' "$color" "$(date '+%Y-%m-%d %H:%M:%S')" "$message" "$NC"
}

# 错误处理函数
die() {
    log "$RED" "错误: $*"
    exit 1
}

# 确认指定 remote 指向预期仓库。
ensure_remote() {
    local name=$1
    local url=$2
    if git remote get-url "$name" >/dev/null 2>&1; then
        git remote set-url "$name" "$url"
    else
        git remote add "$name" "$url"
    fi
}

# 判断状态清单中是否已经记录提交。
state_has() {
    local bucket=$1
    local commit=$2
    jq -e --arg bucket "$bucket" --arg commit "$commit" \
        '((.[$bucket] // []) | map(select(.commit == $commit)) | length) > 0' \
        "$STATE_FILE" >/dev/null
}

# 向 integrated 数组写入一条状态，并清理同一提交的 pending 记录。
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

# 记录无需自动移植的文档或版本提交。
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

# 记录等待人工审查的提交；脚本会在这里停止，避免跳过依赖关系。
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

# 判断提交是否只包含文档、版本号或发布元数据。
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

# 只允许小型、非删除的代码提交自动进入同步分支。
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

# 写入面向 PR 和 Actions 摘要的同步报告。
write_report() {
    mkdir -p "$(dirname "$REPORT_FILE")"
    {
        printf '# 官方与子分支同步报告\n\n'
        printf -- '- 官方仓库：`%s`\n' "$UPSTREAM_URL"
        printf -- '- 官方基线：`%s`\n' "$(git rev-parse "refs/remotes/upstream/$UPSTREAM_REF")"
        printf -- '- 子分支仓库：`%s`\n' "$CHILD_URL"
        printf -- '- 子分支基线：`%s`\n' "$(git rev-parse "refs/remotes/child/$CHILD_REF")"
        printf -- '- 同步分支：`%s`\n\n' "$SYNC_BRANCH"
        printf '## 已移植\n\n'
        if ((${#APPLIED[@]} == 0)); then
            printf -- '- 本轮没有新的自动移植提交。\n'
        else
            printf '%s\n' "${APPLIED[@]}"
        fi
        printf '\n## 已排除\n\n'
        if ((${#EXCLUDED[@]} == 0)); then
            printf -- '- 本轮没有新增排除提交。\n'
        else
            printf '%s\n' "${EXCLUDED[@]}"
        fi
        printf '\n## 等待人工审查\n\n'
        if ((${#PENDING[@]} == 0)); then
            printf -- '- 没有阻塞项。\n'
        else
            printf '%s\n' "${PENDING[@]}"
        fi
        printf '\n> 官方更新先进入同步分支；子分支只按状态清单和安全规则选择性移植。请审查此 PR 后再合并到 `main`。\n'
    } > "$REPORT_FILE"

    if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
        cat "$REPORT_FILE" >> "$GITHUB_STEP_SUMMARY"
    fi
}

main() {
    command -v git >/dev/null 2>&1 || die "缺少 git"
    command -v jq >/dev/null 2>&1 || die "缺少 jq"
    [[ -f "$STATE_FILE" ]] || die "缺少状态清单: $STATE_FILE"
    jq -e . "$STATE_FILE" >/dev/null || die "状态清单不是有效 JSON: $STATE_FILE"

    [[ -z "$(git status --porcelain)" ]] || die "运行同步前工作区必须干净"
    git config user.name "ganzizi-sync[bot]"
    git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
    ensure_remote upstream "$UPSTREAM_URL"
    ensure_remote child "$CHILD_URL"

    log "$BLUE" "抓取官方、子分支和现有同步分支"
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
        die "同步分支与本 fork main 合并冲突，请先人工处理"
    fi
    if ! git merge --no-edit "refs/remotes/upstream/$UPSTREAM_REF" >/dev/null; then
        git merge --abort || true
        write_report
        die "官方更新与同步分支合并冲突，请先人工处理"
    fi
    local after_official
    after_official=$(git rev-parse HEAD)
    if [[ "$before_official" != "$after_official" ]]; then
        log "$GREEN" "已将官方更新合并到同步分支"
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
            PENDING+=("- \`$commit\` $subject：状态清单已标记为待审查")
            BLOCKED=true
            break
        fi

        subject=$(git show -s --format='%s' "$commit")
        files=$(git diff-tree --root --no-commit-id --name-only -r "$commit" | paste -sd ', ' -)
        if is_nonfunctional_commit "$commit"; then
            reason="仅文档、版本号或发布元数据，不自动改变本 fork 行为"
            state_add_excluded "$commit" "$reason"
            EXCLUDED+=("- \`$commit\` $subject：$reason")
            continue
        fi
        if ! is_safe_code_commit "$commit"; then
            reason="变更范围包含敏感路径、删除操作或超过自动审查规模（文件：$files）"
            state_add_pending "$commit" "$reason"
            PENDING+=("- \`$commit\` $subject：$reason")
            BLOCKED=true
            break
        fi

        log "$CYAN" "尝试移植子分支提交 $commit $subject"
        if git cherry-pick -x "$commit"; then
            state_add_integrated "$commit" "auto" "代码路径符合同步安全规则，已自动 cherry-pick"
            APPLIED+=("- \`$commit\` $subject")
            continue
        fi

        local cherry_head
        cherry_head=$(git rev-parse --git-path CHERRY_PICK_HEAD)
        if [[ -f "$cherry_head" ]] && [[ -z "$(git diff --name-only --diff-filter=U)" ]]; then
            git cherry-pick --skip
            state_add_integrated "$commit" "equivalent" "cherry-pick 为空，当前同步分支已有等价内容"
            APPLIED+=("- \`$commit\` $subject（已有等价内容）")
            continue
        fi

        git cherry-pick --abort || true
        reason="cherry-pick 发生冲突，需要人工按官方版本重新移植"
        state_add_pending "$commit" "$reason"
        PENDING+=("- \`$commit\` $subject：$reason")
        BLOCKED=true
        break
    done

    if ! git diff --quiet -- "$STATE_FILE"; then
        git add "$STATE_FILE"
        git commit -m "记录官方与子分支同步状态"
    fi

    write_report
    local has_changes=false
    if ! git diff --quiet "refs/remotes/origin/main" HEAD; then
        has_changes=true
    fi

    if [[ "$has_changes" == true ]]; then
        log "$BLUE" "推送同步分支 $SYNC_BRANCH（仅快进，不强制覆盖）"
        git push origin "HEAD:refs/heads/$SYNC_BRANCH"
    else
        log "$GREEN" "没有需要推送的文件变更"
    fi

    if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
        printf 'sync_branch=%s\n' "$SYNC_BRANCH" >> "$GITHUB_OUTPUT"
        printf 'has_changes=%s\n' "$has_changes" >> "$GITHUB_OUTPUT"
        printf 'report_file=%s\n' "$REPORT_FILE" >> "$GITHUB_OUTPUT"
        printf 'blocked=%s\n' "$BLOCKED" >> "$GITHUB_OUTPUT"
    fi

    if [[ "$BLOCKED" == true ]]; then
        log "$YELLOW" "同步已停在待审查提交，官方更新和状态报告仍已保留在同步分支"
    fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
