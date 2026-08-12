#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

assert_rejected() {
  local expected="$1"
  shift
  local output exit_code=0
  output="$(env \
    -u GITHUB_ACTIONS \
    -u GITHUB_WORKFLOW_REF \
    -u GITHUB_SHA \
    -u SHARESUB_DEPLOY_HOST \
    -u SHARESUB_DEPLOY_DIR \
    -u SHARESUB_DEPLOY_VIA_GITHUB_ACTIONS \
    -u SHARESUB_DEPLOY_ALLOW_MIGRATIONS \
    "$@" "$ROOT/scripts/deploy.sh" deploy 2>&1)" || exit_code=$?
  [[ "$exit_code" -ne 0 ]] || {
    printf '部署脚本错误接受了未授权调用\n' >&2
    exit 1
  }
  [[ "$output" == "错误：$expected" ]] || {
    printf '部署脚本拒绝原因不正确：%s\n' "$output" >&2
    exit 1
  }
}

assert_rejected "生产操作只能通过 GitHub Actions 的 Deploy production workflow 执行" \
  GITHUB_ACTIONS=false
assert_rejected "当前不是 main 分支的 Deploy production workflow" \
  GITHUB_ACTIONS=true GITHUB_WORKFLOW_REF=owner/repo/.github/workflows/other.yml@refs/heads/main
assert_rejected "缺少 GitHub 生产部署授权标记" \
  GITHUB_ACTIONS=true GITHUB_WORKFLOW_REF=owner/repo/.github/workflows/deploy-production.yml@refs/heads/main
assert_rejected "缺少 SHARESUB_DEPLOY_HOST" \
  GITHUB_ACTIONS=true GITHUB_WORKFLOW_REF=owner/repo/.github/workflows/deploy-production.yml@refs/heads/main \
  SHARESUB_DEPLOY_VIA_GITHUB_ACTIONS=1
assert_rejected "缺少 SHARESUB_DEPLOY_DIR" \
  GITHUB_ACTIONS=true GITHUB_WORKFLOW_REF=owner/repo/.github/workflows/deploy-production.yml@refs/heads/main \
  SHARESUB_DEPLOY_VIA_GITHUB_ACTIONS=1 SHARESUB_DEPLOY_HOST=deploy@example.com

printf '生产部署入口拒绝测试通过\n'
