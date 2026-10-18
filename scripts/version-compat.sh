#!/usr/bin/env bash
# 校验或同步 Grok2API 的发行版本号，避免上游已发布新版本但 VERSION 未更新。

set -euo pipefail

VERSION_FILE="${GROK2API_VERSION_FILE:-VERSION}"
RELEASE_API="${GROK2API_RELEASE_API:-https://api.github.com/repos/chenyme/grok2api/releases/latest}"
VERIFY_REF="${GROK2API_VERSION_VERIFY_REF:-}"
TAG_REMOTE="${GROK2API_VERSION_TAG_REMOTE:-upstream}"

die() {
    printf '版本兼容检查失败：%s\n' "$*" >&2
    exit 1
}

require_commands() {
    local command_name
    for command_name in curl jq python3; do
        command -v "$command_name" >/dev/null 2>&1 || die "缺少命令 $command_name"
    done
}

read_version() {
    [[ -f "$VERSION_FILE" ]] || die "版本文件不存在：$VERSION_FILE"
    tr -d '\r\n' < "$VERSION_FILE"
}

latest_release_tag() {
    local payload tag
    payload=$(curl -fsSL --max-time 15 \
        -H 'Accept: application/vnd.github+json' \
        -H 'X-GitHub-Api-Version: 2022-11-28' \
        "$RELEASE_API") || die "无法读取 GitHub 最新 Release"
    tag=$(jq -er 'select(.draft == false and .prerelease == false) | .tag_name' <<< "$payload") \
        || die "GitHub 最新 Release 缺少稳定 tag"
    [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-hotfix\.[0-9]+)?$ ]] \
        || die "最新 Release tag 不是受支持的稳定版本：$tag"
    printf '%s\n' "$tag"
}

compare_versions() {
    python3 - "$1" "$2" <<'PY'
import re
import sys

pattern = re.compile(r"^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$")

def parse(value):
    match = pattern.fullmatch(value.strip())
    if not match:
        return None
    numbers = tuple(int(match.group(index)) for index in range(1, 4))
    prerelease = match.group(4)
    return numbers, prerelease

current = parse(sys.argv[1])
latest = parse(sys.argv[2])
if current is None or latest is None:
    print("invalid")
    raise SystemExit(0)

if current[0] != latest[0]:
    print("-1" if current[0] < latest[0] else "1")
else:
    # lab/hotfix 后缀不改变同一数字发行线的兼容性。
    print("0")
PY
}

verify_tag_ancestor() {
    local latest_tag=$1
    local tag_commit
    [[ -n "$VERIFY_REF" ]] || return 0
    command -v git >/dev/null 2>&1 || die "同步模式需要 git"
    git fetch --no-tags "$TAG_REMOTE" "refs/tags/$latest_tag:refs/tags/$latest_tag" >/dev/null 2>&1 \
        || die "无法从 $TAG_REMOTE 获取版本 tag $latest_tag"
    tag_commit=$(git rev-list -n 1 "$latest_tag^{commit}") \
        || die "无法解析版本 tag $latest_tag"
    git merge-base --is-ancestor "$tag_commit" "$VERIFY_REF" \
        || die "版本 tag $latest_tag 不在目标上游分支历史中"
}

check_version() {
    local current_version latest_tag comparison
    require_commands
    current_version=$(read_version)
    latest_tag=$(latest_release_tag)
    comparison=$(compare_versions "$current_version" "$latest_tag")
    if [[ "$comparison" == "invalid" || "$comparison" == "-1" ]]; then
        die "当前版本 $current_version 低于最新稳定版本 $latest_tag"
    fi
    printf '版本兼容检查通过：当前 %s，最新稳定版本 %s\n' "$current_version" "$latest_tag"
}

sync_version() {
    local current_version latest_tag comparison
    require_commands
    current_version=$(read_version)
    latest_tag=$(latest_release_tag)
    verify_tag_ancestor "$latest_tag"
    comparison=$(compare_versions "$current_version" "$latest_tag")
    if [[ "$comparison" == "invalid" || "$comparison" == "-1" ]]; then
        printf '%s\n' "$latest_tag" > "$VERSION_FILE"
        printf '版本已同步：%s -> %s\n' "${current_version:-<空>}" "$latest_tag"
        return 0
    fi
    printf '版本无需同步：当前 %s，最新稳定版本 %s\n' "$current_version" "$latest_tag"
}

case "${1:-}" in
    --check)
        check_version
        ;;
    --sync)
        sync_version
        ;;
    *)
        printf '用法：%s --check|--sync\n' "$0" >&2
        exit 2
        ;;
esac
