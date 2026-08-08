#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="$ROOT/VERSION"

die() {
  printf '错误：%s\n' "$1" >&2
  exit 1
}

[[ -f "$VERSION_FILE" ]] || die "缺少 VERSION 文件"

version="$(<"$VERSION_FILE")"
semver_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*)|([0-9]*[A-Za-z-][0-9A-Za-z-]*))(\.((0|[1-9][0-9]*)|([0-9]*[A-Za-z-][0-9A-Za-z-]*)))*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
[[ "$version" =~ $semver_pattern ]] \
  || die "VERSION 必须是不带 v 前缀的 SemVer，当前值：$version"

if [[ "${1:-}" == "--release" ]]; then
  tag="v$version"
  git -C "$ROOT" rev-parse --verify --quiet "refs/tags/$tag" >/dev/null \
    || die "当前版本缺少发布标签：$tag"
  [[ "$(git -C "$ROOT" cat-file -t "refs/tags/$tag")" == "tag" ]] \
    || die "发布标签必须是 annotated tag：$tag"
  [[ "$(git -C "$ROOT" rev-list -n 1 "$tag")" == "$(git -C "$ROOT" rev-parse HEAD)" ]] \
    || die "发布标签 $tag 未指向当前提交"
elif [[ $# -ne 0 ]]; then
  die "用法：scripts/verify-version.sh [--release]"
fi

printf '%s\n' "$version"
