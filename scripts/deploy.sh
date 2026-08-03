#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_HOST="underelay"
DEPLOY_DIR="/home/cqcpcqp/share2api"
COMPOSE="docker compose --env-file .env -f deploy/docker-compose.yml"
PUBLIC_HEALTH_URL="https://share.underelay.com/health"
EXPECTED_HEALTH='{"status":"ok"}'
DEPLOYED_COMMIT_FILE="$DEPLOY_DIR/.deployed-commit"

info() {
  printf '\n==> %s\n' "$1"
}

die() {
  printf '错误：%s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
}

usage() {
  cat <<'EOF'
用法：scripts/deploy.sh <deploy|status|logs|backup>

  deploy  验证、备份并发布当前 main 提交
  status  查看生产容器和公网健康状态
  logs    持续查看生产日志
  backup  立即创建一次生产数据库备份
EOF
}

read_deployed_commit() {
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "test -f '$DEPLOYED_COMMIT_FILE' && cat '$DEPLOYED_COMMIT_FILE'"
}

wait_for_remote_health() {
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -u; for attempt in \$(seq 1 30); do body=\$(curl -fsS http://127.0.0.1:8081/health 2>/dev/null || true); if [ \"\$body\" = '$EXPECTED_HEALTH' ]; then exit 0; fi; sleep 2; done; exit 1"
}

create_backup() {
  local label="$1"
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; install -d -m 700 backups; backup=\"backups/sharesub-${label}-\$(date +%Y%m%d-%H%M%S).dump\"; $COMPOSE exec -T postgres pg_dump -U sharesub -d sharesub -Fc > \"\$backup\"; chmod 600 \"\$backup\"; $COMPOSE exec -T postgres pg_restore --list < \"\$backup\" >/dev/null; printf '%s/%s\n' '$DEPLOY_DIR' \"\$backup\""
}

restore_previous_images() {
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; docker image tag sharesub-api:previous sharesub-api:latest; docker image tag sharesub-web:previous sharesub-web:latest"
}

rollback_running_services() {
  restore_previous_images
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; $COMPOSE up -d --no-build --force-recreate api web"
}

run_status() {
  require_command ssh
  require_command curl

  local deployed_commit health
  deployed_commit="$(read_deployed_commit)" || die "服务器尚未记录已部署版本"
  printf '已部署提交：%s\n' "$deployed_commit"

  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; $COMPOSE ps"

  health="$(curl -fsS --connect-timeout 10 --max-time 30 "$PUBLIC_HEALTH_URL")" \
    || die "公网健康检查失败：$PUBLIC_HEALTH_URL"
  [[ "$health" == "$EXPECTED_HEALTH" ]] \
    || die "公网健康检查返回非预期内容：$health"
  printf '公网健康检查：%s\n' "$health"
}

run_logs() {
  require_command ssh
  exec ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "cd '$DEPLOY_DIR' && $COMPOSE logs -f --tail=200 api web postgres"
}

run_backup() {
  require_command ssh
  info "备份生产数据库"
  local backup_path
  backup_path="$(create_backup manual)"
  printf '备份完成：%s\n' "$backup_path"
}

assert_deployable_commit() {
  local branch worktree_status head_commit origin_commit

  branch="$(git -C "$ROOT" branch --show-current)"
  [[ "$branch" == "main" ]] || die "只能从 main 发布，当前分支：$branch"

  worktree_status="$(git -C "$ROOT" status --porcelain --untracked-files=all)"
  [[ -z "$worktree_status" ]] || die "工作区不干净，请先提交或移除本地改动"

  info "更新 origin/main" >&2
  GIT_SSH_COMMAND="ssh -o BatchMode=yes" git -C "$ROOT" fetch --quiet origin main

  head_commit="$(git -C "$ROOT" rev-parse HEAD)"
  origin_commit="$(git -C "$ROOT" rev-parse origin/main)"
  [[ "$head_commit" == "$origin_commit" ]] \
    || die "HEAD 尚未推送到 origin/main"

  printf '%s\n' "$head_commit"
}

run_deploy() {
  require_command git
  require_command make
  require_command rsync
  require_command ssh
  require_command tar
  require_command curl

  local current_commit previous_commit short_commit release_dir backup_path
  local migrations_changed=0

  current_commit="$(assert_deployable_commit)"
  short_commit="${current_commit:0:12}"
  previous_commit="$(read_deployed_commit)" \
    || die "服务器尚未记录已部署版本，禁止猜测升级基线"

  [[ "$previous_commit" =~ ^[0-9a-f]{40}$ ]] \
    || die "服务器记录的提交号无效：$previous_commit"
  git -C "$ROOT" cat-file -e "$previous_commit^{commit}" 2>/dev/null \
    || die "本地仓库找不到服务器当前提交：$previous_commit"

  if [[ "$current_commit" == "$previous_commit" ]]; then
    printf '版本 %s 已经部署，无需重复发布。\n' "$short_commit"
    run_status
    return
  fi

  if ! git -C "$ROOT" diff --quiet "$previous_commit" "$current_commit" -- backend/migrations; then
    migrations_changed=1
    [[ "${SHARESUB_DEPLOY_ALLOW_MIGRATIONS:-}" == "1" ]] \
      || die "检测到数据库迁移变更；确认后使用 SHARESUB_DEPLOY_ALLOW_MIGRATIONS=1 make deploy"
  fi

  info "运行完整测试"
  make -C "$ROOT" test

  release_dir="$(mktemp -d "${TMPDIR:-/tmp}/sharesub-release.XXXXXX")"
  trap "rm -rf '$release_dir'" EXIT
  git -C "$ROOT" archive --format=tar "$current_commit" | tar -xf - -C "$release_dir"

  info "同步提交 $short_commit"
  rsync -az --delete-after --itemize-changes \
    --exclude='.env' \
    --exclude='.deployed-commit' \
    --exclude='backups/' \
    "$release_dir/" "$DEPLOY_HOST:$DEPLOY_DIR/"

  info "校验生产配置"
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; test \"\$(stat -c %a .env)\" = 600; $COMPOSE config --quiet; cmp -s deploy/nginx-share.underelay.com.conf /etc/nginx/sites-available/share.underelay.com" \
    || die "生产配置校验失败，或仓库内 Nginx 配置尚未安装"

  info "保存当前镜像"
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; docker image tag sharesub-api:latest sharesub-api:previous; docker image tag sharesub-web:latest sharesub-web:previous"

  info "构建 API 镜像"
  if ! ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; $COMPOSE build api"; then
    restore_previous_images
    die "API 镜像构建失败，当前服务未切换"
  fi

  info "构建 Web 镜像"
  if ! ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; $COMPOSE build web"; then
    restore_previous_images
    die "Web 镜像构建失败，当前服务未切换"
  fi

  info "备份生产数据库"
  if ! backup_path="$(create_backup "$short_commit")"; then
    restore_previous_images
    die "数据库备份失败，当前服务未切换"
  fi
  printf '数据库备份：%s\n' "$backup_path"

  info "切换到新镜像"
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "set -eu; cd '$DEPLOY_DIR'; $COMPOSE up -d --no-build"

  if ! wait_for_remote_health; then
    ssh -o BatchMode=yes "$DEPLOY_HOST" \
      "cd '$DEPLOY_DIR' && $COMPOSE logs --no-color --tail=120 api web postgres" || true
    if [[ "$migrations_changed" -eq 0 ]]; then
      info "健康检查失败，恢复上一组镜像"
      rollback_running_services
      wait_for_remote_health || die "新版本失败，且上一版本恢复后的健康检查也失败"
      die "新版本健康检查失败，已恢复上一版本"
    fi
    die "新版本健康检查失败；迁移版本不会自动回滚，数据库备份位于 $backup_path"
  fi

  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "printf '%s\n' '$current_commit' > '$DEPLOYED_COMMIT_FILE' && chmod 644 '$DEPLOYED_COMMIT_FILE'"

  local public_health
  public_health="$(curl -fsS --connect-timeout 10 --max-time 30 "$PUBLIC_HEALTH_URL")" \
    || die "新版本已启动，但公网健康检查失败"
  [[ "$public_health" == "$EXPECTED_HEALTH" ]] \
    || die "新版本已启动，但公网健康检查返回非预期内容：$public_health"

  curl -fsS --connect-timeout 10 --max-time 30 -o /dev/null https://www.underelay.com/health
  curl -fsS --connect-timeout 10 --max-time 30 -o /dev/null https://stats.underelay.com/

  info "发布完成"
  printf '版本：%s\n' "$current_commit"
  printf '健康检查：%s\n' "$public_health"
  printf '数据库备份：%s\n' "$backup_path"
  ssh -o BatchMode=yes "$DEPLOY_HOST" \
    "cd '$DEPLOY_DIR' && $COMPOSE ps"
}

case "${1:-}" in
  deploy) run_deploy ;;
  status) run_status ;;
  logs) run_logs ;;
  backup) run_backup ;;
  *) usage; exit 1 ;;
esac
