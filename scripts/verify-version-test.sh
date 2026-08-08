#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sharesub-version-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT

mkdir -p "$TEST_ROOT/scripts"
cp "$ROOT/scripts/verify-version.sh" "$TEST_ROOT/scripts/verify-version.sh"

assert_valid() {
  local version="$1" actual
  printf '%s\n' "$version" > "$TEST_ROOT/VERSION"
  actual="$("$TEST_ROOT/scripts/verify-version.sh")"
  [[ "$actual" == "$version" ]] || {
    printf '合法版本 %s 的输出不正确：%s\n' "$version" "$actual" >&2
    exit 1
  }
}

assert_invalid() {
  local version="$1"
  printf '%s\n' "$version" > "$TEST_ROOT/VERSION"
  if "$TEST_ROOT/scripts/verify-version.sh" >/dev/null 2>&1; then
    printf '非法版本被错误接受：%s\n' "$version" >&2
    exit 1
  fi
}

valid_versions=(
  '0.1.0'
  '1.0.0-alpha'
  '1.0.0-alpha--beta'
  '1.0.0-0.3.7'
  '1.0.0-x.7.z.92'
  '1.0.0+build.1'
  '1.0.0-alpha+001'
)

invalid_versions=(
  'v1.0.0'
  '01.0.0'
  '1.0.0-01'
  '1.0.0-alpha..1'
  '1.0.0-alpha_1'
  '1.0.0+'
)

for version in "${valid_versions[@]}"; do
  assert_valid "$version"
done

for version in "${invalid_versions[@]}"; do
  assert_invalid "$version"
done

printf '版本格式测试通过（%d 个合法样例，%d 个非法样例）\n' \
  "${#valid_versions[@]}" "${#invalid_versions[@]}"
