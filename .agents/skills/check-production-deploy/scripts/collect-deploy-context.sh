#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

usage() {
  printf 'Usage: %s [repository-root]\n' "${0##*/}"
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi
[[ $# -le 1 ]] || { usage >&2; exit 2; }

require_command git
require_command curl
require_command python3

root="${1:-.}"
root="$(git -C "$root" rev-parse --show-toplevel 2>/dev/null)" || die "not inside a Git repository"
remote_url="$(git -C "$root" remote get-url origin 2>/dev/null)" || die "origin remote is missing"
case "$remote_url" in
  git@github.com:cqcpcqp/sharesub.git|https://github.com/cqcpcqp/sharesub.git|ssh://git@github.com/cqcpcqp/sharesub.git) ;;
  *) die "origin is not cqcpcqp/sharesub: $remote_url" ;;
esac

git -C "$root" fetch origin main --tags

repo="cqcpcqp/sharesub"
api_root="https://api.github.com/repos/$repo"
curl_args=(
  -fsSL
  --connect-timeout 10
  --max-time 30
  -H "Accept: application/vnd.github+json"
  -H "X-GitHub-Api-Version: 2022-11-28"
  -H "User-Agent: sharesub-production-deploy-check"
)
github_token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [[ -n "$github_token" ]]; then
  curl_args+=(-H "Authorization: Bearer $github_token")
fi

deploy_json="$(curl "${curl_args[@]}" \
  "$api_root/actions/workflows/deploy-production.yml/runs?status=success&per_page=20")" \
  || die "cannot read successful production workflow runs from GitHub"
run_tsv="$(python3 -c '
import json, sys
runs = json.load(sys.stdin).get("workflow_runs", [])
run = next((r for r in runs if r.get("conclusion") == "success"), None)
if run:
    print("\t".join(str(run.get(k, "")) for k in ("id", "head_sha", "created_at", "updated_at", "html_url")))
' <<<"$deploy_json")"
[[ -n "$run_tsv" ]] || die "no successful Deploy production workflow run found"

IFS=$'\t' read -r deploy_run_id baseline deploy_created_at deploy_updated_at deploy_url <<<"$run_tsv"
[[ "$baseline" =~ ^[0-9a-f]{40}$ ]] || die "deployment run returned an invalid head SHA"
git -C "$root" cat-file -e "$baseline^{commit}" 2>/dev/null \
  || git -C "$root" fetch --no-tags origin "$baseline"
git -C "$root" cat-file -e "$baseline^{commit}" 2>/dev/null \
  || die "deployment baseline commit is unavailable locally"

target="$(git -C "$root" rev-parse origin/main)"
head="$(git -C "$root" rev-parse HEAD)"
branch="$(git -C "$root" branch --show-current)"
[[ -n "$branch" ]] || branch="(detached)"

if git -C "$root" merge-base --is-ancestor "$baseline" "$target"; then
  baseline_is_ancestor=true
else
  baseline_is_ancestor=false
fi

version="$(git -C "$root" show "$target:VERSION")"
tag="v$version"
tag_valid=false
if git -C "$root" rev-parse --verify --quiet "refs/tags/$tag" >/dev/null \
  && [[ "$(git -C "$root" cat-file -t "refs/tags/$tag")" == "tag" ]] \
  && [[ "$(git -C "$root" rev-list -n 1 "$tag")" == "$target" ]]; then
  tag_valid=true
fi

ci_json="$(curl "${curl_args[@]}" \
  "$api_root/actions/workflows/ci-images.yml/runs?branch=main&event=push&status=success&per_page=100")" \
  || die "cannot read successful CI/images workflow runs from GitHub"
ci_tsv="$(TARGET_SHA="$target" python3 -c '
import json, os, sys
target = os.environ["TARGET_SHA"]
runs = json.load(sys.stdin).get("workflow_runs", [])
run = next((r for r in runs if r.get("conclusion") == "success" and r.get("event") == "push" and r.get("head_sha") == target), None)
if run:
    print("\t".join(str(run.get(k, "")) for k in ("id", "created_at", "updated_at", "html_url")))
' <<<"$ci_json")"
[[ -n "$ci_tsv" ]] || die "no successful push CI/images run found for origin/main $target"
IFS=$'\t' read -r ci_run_id ci_created_at ci_updated_at ci_url <<<"$ci_tsv"

if git -C "$root" diff --quiet "$baseline" "$target" -- backend/migrations; then
  migrations_changed=false
else
  migrations_changed=true
fi

if [[ -z "$(git -C "$root" status --porcelain --untracked-files=all)" ]]; then
  worktree_clean=true
else
  worktree_clean=false
fi

printf 'repository=%s\n' "$repo"
printf 'deploy_run_id=%s\n' "$deploy_run_id"
printf 'deploy_run_url=%s\n' "$deploy_url"
printf 'deploy_created_at=%s\n' "$deploy_created_at"
printf 'deploy_completed_at=%s\n' "$deploy_updated_at"
printf 'baseline=%s\n' "$baseline"
printf 'target=%s\n' "$target"
printf 'baseline_is_ancestor=%s\n' "$baseline_is_ancestor"
printf 'current_branch=%s\n' "$branch"
printf 'current_head=%s\n' "$head"
printf 'worktree_clean=%s\n' "$worktree_clean"
printf 'release_tag=%s\n' "$tag"
printf 'release_tag_valid=%s\n' "$tag_valid"
printf 'ci_images_run_id=%s\n' "$ci_run_id"
printf 'ci_images_run_url=%s\n' "$ci_url"
printf 'ci_images_created_at=%s\n' "$ci_created_at"
printf 'ci_images_completed_at=%s\n' "$ci_updated_at"
printf 'migrations_changed=%s\n' "$migrations_changed"

printf '\nMigration path changes:\n'
git -C "$root" diff --name-status "$baseline" "$target" -- backend/migrations

printf '\nRelease commits:\n'
git -C "$root" log --oneline --decorate "$baseline..$target"
