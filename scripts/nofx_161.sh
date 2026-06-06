#!/usr/bin/env bash
set -Eeuo pipefail

REMOTE_HOST="${NOFX_DEPLOY_HOST:-43.167.168.161}"
REMOTE_USER="${NOFX_DEPLOY_USER:-ubuntu}"
REMOTE_DIR="${NOFX_REMOTE_DIR:-/home/ubuntu/appai3/nofx}"
BRANCH="${NOFX_BRANCH:-jzhbnofxdev}"
ACTION="${1:-deploy}"

SSH_ARGS=(-o StrictHostKeyChecking=accept-new)
if [[ -n "${NOFX_SSH_KEY:-}" ]]; then
  SSH_ARGS+=(-i "$NOFX_SSH_KEY")
fi

REMOTE="${REMOTE_USER}@${REMOTE_HOST}"

usage() {
  cat <<'USAGE'
Usage:
  scripts/nofx_161.sh deploy   # pull latest code, rebuild, restart, health-check
  scripts/nofx_161.sh status   # show git revision, compose status, health-check
  scripts/nofx_161.sh shell    # open an interactive shell in the remote project

Environment overrides:
  NOFX_DEPLOY_HOST  default: 43.167.168.161
  NOFX_DEPLOY_USER  default: ubuntu
  NOFX_REMOTE_DIR   default: /home/ubuntu/appai3/nofx
  NOFX_BRANCH       default: jzhbnofxdev
  NOFX_SSH_KEY      optional SSH private key path
USAGE
}

remote_bash() {
  ssh "${SSH_ARGS[@]}" "$REMOTE" \
    "NOFX_REMOTE_DIR=$(printf '%q' "$REMOTE_DIR") NOFX_BRANCH=$(printf '%q' "$BRANCH") bash -s"
}

case "$ACTION" in
  deploy)
    remote_bash <<'REMOTE'
set -Eeuo pipefail

cd "$NOFX_REMOTE_DIR"

echo "==> Remote: $(hostname)"
echo "==> Project: $(pwd)"
echo "==> Branch: $NOFX_BRANCH"

current_branch="$(git branch --show-current)"
if [[ "$current_branch" != "$NOFX_BRANCH" ]]; then
  echo "ERROR: current branch is '$current_branch', expected '$NOFX_BRANCH'." >&2
  exit 1
fi

tracked_changes="$(git status --porcelain --untracked-files=no)"
if [[ -n "$tracked_changes" ]]; then
  echo "ERROR: tracked files have local changes. Refusing to pull automatically." >&2
  echo "$tracked_changes" >&2
  exit 1
fi

echo "==> Pull latest code"
git pull --ff-only origin "$NOFX_BRANCH"

echo "==> Rebuild and restart docker services"
sudo docker compose up -d --build

echo "==> Compose status"
sudo docker compose ps

echo "==> Backend health"
curl -fsS http://127.0.0.1:8080/health
printf '\n'

echo "==> Frontend health"
curl -fsS -o /dev/null -w 'HTTP %{http_code}\n' http://127.0.0.1:3000/

echo "==> Recent backend logs (redacted)"
sudo docker compose logs --tail=80 nofx \
  | sed -E 's/([A-Za-z0-9_]*(KEY|TOKEN|SECRET|PASSWORD)[A-Za-z0-9_]*)(=|: )[[:graph:]]+/\1\3[REDACTED]/Ig; s/(Bearer )[[:graph:]]+/\1[REDACTED]/Ig; s/sk-[A-Za-z0-9]+/[REDACTED]/g'
REMOTE
    ;;

  status)
    remote_bash <<'REMOTE'
set -Eeuo pipefail

cd "$NOFX_REMOTE_DIR"

echo "==> Remote: $(hostname)"
echo "==> Project: $(pwd)"
echo "==> Branch: $(git branch --show-current)"
echo "==> Commit: $(git rev-parse --short HEAD)"
echo "==> Git status"
git status --short

echo "==> Compose status"
sudo docker compose ps

echo "==> Backend health"
curl -fsS http://127.0.0.1:8080/health
printf '\n'

echo "==> Frontend health"
curl -fsS -o /dev/null -w 'HTTP %{http_code}\n' http://127.0.0.1:3000/
REMOTE
    ;;

  shell)
    ssh -tt "${SSH_ARGS[@]}" "$REMOTE" "cd $(printf '%q' "$REMOTE_DIR") && exec bash -l"
    ;;

  -h|--help|help)
    usage
    ;;

  *)
    echo "ERROR: unknown action '$ACTION'." >&2
    usage >&2
    exit 2
    ;;
esac
