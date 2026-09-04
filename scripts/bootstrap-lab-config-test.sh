#!/usr/bin/env bash
# Verify that bootstrap keeps the administrator password out of stdout.

set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd -P)
repo_root=$(cd "$script_dir/.." && pwd -P)
temp_root=$(mktemp -d "${TMPDIR:-/tmp}/grok2api-bootstrap-test.XXXXXX")

cleanup() {
    rm -rf "$temp_root"
}
trap cleanup EXIT

fail() {
    printf 'Test failed: %s\n' "$1" >&2
    exit 1
}

file_mode() {
    case "$(uname -s)" in
        Darwin) stat -f '%Lp' "$1" ;;
        *) stat -c '%a' "$1" ;;
    esac
}

mkdir -p "$temp_root/scripts"
cp "$repo_root/config.example.yaml" "$temp_root/config.example.yaml"
cp "$repo_root/scripts/bootstrap-lab-config.sh" "$temp_root/scripts/bootstrap-lab-config.sh"

output=$(cd "$temp_root/scripts" && sh ./bootstrap-lab-config.sh)
password_file="$temp_root/admin-password.txt"
config_file="$temp_root/config.yaml"

[ -s "$password_file" ] || fail "administrator password file is missing or empty"
[ -s "$config_file" ] || fail "generated configuration file is missing or empty"
[ "$(file_mode "$password_file")" = "600" ] || fail "administrator password file mode is not 600"
[ "$(file_mode "$config_file")" = "600" ] || fail "configuration file mode is not 600"

stored_password=$(sed -n '1p' "$password_file")
config_password=$(sed -n 's/^  password: "\(.*\)"$/\1/p' "$config_file")
[ -n "$stored_password" ] || fail "administrator password file has no valid content"
[ "$stored_password" = "$config_password" ] || fail "password file does not match the configuration"

if printf '%s\n' "$output" | grep -Fq "$stored_password"; then
    fail "script output contains the administrator password"
fi
if printf '%s\n' "$output" | grep -Fq 'printed once'; then
    fail "script output still claims to print the administrator password"
fi
printf '%s\n' "$output" | grep -Fq 'admin-password.txt' || fail "script does not identify the protected password file"

printf '%s\n' 'Bootstrap security regression test passed'
