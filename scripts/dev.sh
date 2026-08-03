#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="$ROOT/.run"
ENV_FILE="$ROOT/.env"
COMPOSE=(docker compose --env-file "$ENV_FILE" -f "$ROOT/deploy/docker-compose.yml" -f "$ROOT/deploy/docker-compose.dev.yml")

help() {
  echo "ShareSub 本地开发命令"
  echo "  make dev      一键启动 PostgreSQL、后端和前端"
  echo "  make status   查看服务状态"
  echo "  make logs     持续查看后端和前端日志"
  echo "  make stop     停止前端和后端，保留 PostgreSQL"
  echo "  make down     停止全部服务，保留数据库数据卷"
  echo "  make restart  重启全部本地开发服务"
  echo "  make test     执行后端测试、静态检查和前端构建检查"
}

init_env() {
  if [[ -f "$ENV_FILE" ]]; then
    return
  fi
  command -v openssl >/dev/null || {
    echo "缺少 openssl，未创建 .env"
    exit 1
  }
  local token credential
  token="$(openssl rand -base64 32)"
  credential="$(openssl rand -base64 32)"
  awk -F= -v token="$token" -v credential="$credential" '
    BEGIN { OFS="=" }
    $1 == "SHARESUB_TOKEN_PEPPER" { $2=token }
    $1 == "SHARESUB_CREDENTIAL_KEY" { $2=credential }
    { print }
  ' "$ROOT/.env.example" > "$ENV_FILE.tmp"
  mv "$ENV_FILE.tmp" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  echo "已创建 .env 并生成独立密钥"
}

check() {
  init_env
  local command_name
  for command_name in go node pnpm docker curl; do
    if ! command -v "$command_name" >/dev/null; then
      echo "缺少 $command_name；启动脚本不会自动安装，请先确认安装方式。"
      exit 1
    fi
  done
  if ! docker info >/dev/null 2>&1; then
    echo "Docker daemon 未运行，请先启动 Docker Desktop。"
    exit 1
  fi
  local legacy_postgres legacy_config
  legacy_postgres="$(docker ps -aq --filter label=com.docker.compose.project=deploy --filter label=com.docker.compose.service=postgres | sed -n '1p')"
  if [[ -n "$legacy_postgres" ]]; then
    legacy_config="$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project.config_files" }}' "$legacy_postgres" 2>/dev/null || true)"
    if [[ "$legacy_config" == *"$ROOT/deploy/docker-compose.yml"* ]]; then
      echo "检测到旧版 ShareSub Compose 项目 deploy（容器 $legacy_postgres）。"
      echo "为防止两个 PostgreSQL 容器同时挂载同一数据卷，请先按 README 的迁移步骤停止旧项目。"
      exit 1
    fi
  fi
  if [[ ! -x "$ROOT/frontend/node_modules/.bin/vite" ]]; then
    echo "前端依赖尚未安装；启动脚本不会自动下载依赖。"
    exit 1
  fi
  if ! docker image inspect postgres:17-alpine >/dev/null 2>&1; then
    echo "缺少 postgres:17-alpine 镜像；启动脚本不会自动下载镜像。"
    exit 1
  fi
}

pid_running() {
  local pid_file="$1"
  [[ -f "$pid_file" ]] && kill -0 "$(cat "$pid_file")" 2>/dev/null
}

listener_pid() {
  lsof -t -iTCP:"$1" -sTCP:LISTEN 2>/dev/null | sed -n '1p'
}

assert_port_free() {
  local port="$1" service_name="$2"
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "$service_name 端口 $port 已被其他进程占用，请先处理该进程。"
    exit 1
  fi
}

start_db() {
  mkdir -p "$RUN_DIR"
  "${COMPOSE[@]}" up -d postgres
  local attempt
  for attempt in $(seq 1 30); do
    if "${COMPOSE[@]}" exec -T postgres pg_isready -U sharesub -d sharesub >/dev/null 2>&1; then
      echo "PostgreSQL 已就绪"
      return
    fi
    sleep 1
  done
  echo "PostgreSQL 在 30 秒内未就绪"
  exit 1
}

start_backend() {
  local pid_file="$RUN_DIR/backend.pid"
  if pid_running "$pid_file"; then
    echo "后端已在运行（PID $(cat "$pid_file")）"
    return
  fi
  rm -f "$pid_file"
  assert_port_free 8080 "后端"
  mkdir -p "$RUN_DIR/go-cache"
  (
    cd "$ROOT/backend"
    GOCACHE="$RUN_DIR/go-cache" go build -mod=readonly -o "$RUN_DIR/sharesub-api" ./cmd/api
  )
  set -a
  source "$ENV_FILE"
  set +a
  nohup "$RUN_DIR/sharesub-api" > "$RUN_DIR/backend.log" 2>&1 &
  echo $! > "$pid_file"
  echo "后端已启动（PID $!）"
}

start_frontend() {
  local pid_file="$RUN_DIR/frontend.pid"
  if pid_running "$pid_file"; then
    echo "前端已在运行（PID $(cat "$pid_file")）"
    return
  fi
  rm -f "$pid_file"
  assert_port_free 5173 "前端"
  (
    cd "$ROOT/frontend"
    nohup pnpm exec vite > "$RUN_DIR/frontend.log" 2>&1 &
    echo $! > "$pid_file"
  )
  echo "前端已启动（PID $(cat "$pid_file")）"
}

wait_for_http() {
  local url="$1" pid_file="$2" service_name="$3" log_file="$4"
  local attempt
  for attempt in $(seq 1 30); do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      echo "$service_name 已就绪"
      return
    fi
    if ! pid_running "$pid_file"; then
      echo "$service_name 启动失败，最后日志如下："
      tail -n 30 "$log_file" || true
      exit 1
    fi
    sleep 1
  done
  echo "$service_name 在 30 秒内未就绪，请查看 $log_file"
  exit 1
}

status() {
  local backend_status="未运行" frontend_status="未运行" detected_pid
  if pid_running "$RUN_DIR/backend.pid"; then
    backend_status="运行中（PID $(cat "$RUN_DIR/backend.pid")）"
  else
    detected_pid="$(listener_pid 8080)"
    if [[ -n "$detected_pid" ]]; then
      backend_status="运行中（PID $detected_pid，按监听端口识别）"
    fi
  fi
  if pid_running "$RUN_DIR/frontend.pid"; then
    frontend_status="运行中（PID $(cat "$RUN_DIR/frontend.pid")）"
  else
    detected_pid="$(listener_pid 5173)"
    if [[ -n "$detected_pid" ]]; then
      frontend_status="运行中（PID $detected_pid，按监听端口识别）"
    fi
  fi
  echo "后端：$backend_status"
  echo "前端：$frontend_status"
  if docker info >/dev/null 2>&1 && [[ -f "$ENV_FILE" ]]; then
    "${COMPOSE[@]}" ps postgres
  fi
}

stop_apps() {
  local service_name pid_file pid
  for service_name in frontend backend; do
    pid_file="$RUN_DIR/$service_name.pid"
    if [[ -f "$pid_file" ]]; then
      pid="$(cat "$pid_file")"
      if kill -0 "$pid" 2>/dev/null; then
        pkill -TERM -P "$pid" 2>/dev/null || true
        kill "$pid" 2>/dev/null || true
        echo "已停止 $service_name（PID $pid）"
      fi
      rm -f "$pid_file"
    fi
  done
}

follow_logs() {
  touch "$RUN_DIR/backend.log" "$RUN_DIR/frontend.log"
  echo ""
  echo "正在持续输出日志；按 Ctrl-C 停止前端和后端。"
  trap 'stop_apps; exit 0' INT TERM
  tail -n 20 -f "$RUN_DIR/backend.log" "$RUN_DIR/frontend.log" &
  local tail_pid=$!
  wait "$tail_pid"
}

run_tests() {
  mkdir -p "$RUN_DIR/go-cache"
  (
    cd "$ROOT/backend"
    GOCACHE="$RUN_DIR/go-cache" go test ./...
    GOCACHE="$RUN_DIR/go-cache" go vet ./...
  )
  (
    cd "$ROOT/frontend"
    pnpm typecheck
    pnpm build
  )
}

command_name="${1:-help}"
case "$command_name" in
  help) help ;;
  init) init_env ;;
  check) check ;;
  dev)
    check
    start_db
    start_backend
    wait_for_http "http://127.0.0.1:8080/health" "$RUN_DIR/backend.pid" "后端" "$RUN_DIR/backend.log"
    start_frontend
    wait_for_http "http://127.0.0.1:5173/" "$RUN_DIR/frontend.pid" "前端" "$RUN_DIR/frontend.log"
    status
    echo ""
    echo "ShareSub 已启动："
    echo "  前端：http://127.0.0.1:5173"
    echo "  API：http://127.0.0.1:8080"
    echo "  健康检查：http://127.0.0.1:8080/health"
    echo "另开终端可执行 make status 查看状态。"
    follow_logs
    ;;
  status) status ;;
  logs)
    mkdir -p "$RUN_DIR"
    touch "$RUN_DIR/backend.log" "$RUN_DIR/frontend.log"
    tail -f "$RUN_DIR/backend.log" "$RUN_DIR/frontend.log"
    ;;
  stop) stop_apps ;;
  down)
    stop_apps
    init_env
    "${COMPOSE[@]}" down
    ;;
  restart)
    stop_apps
    init_env
    "${COMPOSE[@]}" down
    "$0" dev
    ;;
  test) run_tests ;;
  *)
    echo "未知命令：$command_name"
    help
    exit 1
    ;;
esac
