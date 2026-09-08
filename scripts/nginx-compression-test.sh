#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
expected_policy='gzip on;
gzip_comp_level 6;
gzip_min_length 256;
gzip_vary on;
gzip_proxied any;
gzip_types text/css text/csv text/javascript text/markdown text/plain text/xml application/json application/javascript application/xml application/rss+xml image/svg+xml;'

for config in frontend/nginx.conf deploy/nginx-share.underelay.com.conf README.md; do
  actual_policy="$(sed 's/#.*$//' "$ROOT/$config" | awk '/^[[:space:]]*gzip[_[:space:]]/ {$1=$1; print}')"
  if [[ "$actual_policy" != "$expected_policy" ]]; then
    printf 'Nginx 普通响应压缩 / SSE 排除策略不符合预期：%s\n' "$config" >&2
    exit 1
  fi
done

printf 'Nginx 压缩配置与文档策略检查通过\n'
