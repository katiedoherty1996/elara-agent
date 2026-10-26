#!/usr/bin/env bash
# preview.sh — run an Elara PR as its own test copy that Katie can open on her phone
# over Tailscale (tailnet only, never public).
#
#   preview.sh up <pr>     build/update PR <pr> and print its address
#   preview.sh down <pr>   stop it and delete its data
#   preview.sh list        show running previews
#   preview.sh sync        remove previews of merged/closed PRs; rebuild ones whose PR changed
#
# Each preview gets a slot (1..MAX): db 5450+slot, web 5190+slot, https 8440+slot.
set -euo pipefail

REPO="${ELARA_REPO:-$HOME/elara}"
ROOT="${PREVIEW_ROOT:-$HOME/previews}"
MAX="${PREVIEW_MAX:-3}"
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
mkdir -p "$ROOT"

host() { tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//'; }
dir_of() { echo "$ROOT/pr-$1"; }
slot_of() { cat "$(dir_of "$1")/.preview-slot" 2>/dev/null || true; }
url_of() { echo "https://$(host):$((8440 + $1))"; }

free_slot() {
  local s used
  used=$(cat "$ROOT"/pr-*/.preview-slot 2>/dev/null || true)
  for s in $(seq 1 "$MAX"); do
    grep -qx "$s" <<<"$used" || { echo "$s"; return; }
  done
}

up() {
  local pr="$1" d slot first=false url
  d=$(dir_of "$pr")
  gh pr view "$pr" -R "$(git -C "$REPO" remote get-url origin)" --json state -q .state | grep -qx OPEN \
    || { echo "PR #$pr isn't open" >&2; exit 1; }
  git -C "$REPO" fetch -q origin "pull/$pr/head:refs/remotes/pr/$pr" --force
  slot=$(slot_of "$pr")
  if [ -z "$slot" ]; then
    slot=$(free_slot)
    [ -n "$slot" ] || { echo "All $MAX preview slots are in use: $(list | tr '\n' ' '). Remove one first." >&2; exit 1; }
    git -C "$REPO" worktree add -q --detach "$d" "refs/remotes/pr/$pr"
    echo "$slot" > "$d/.preview-slot"
    first=true
  else
    git -C "$d" checkout -q --detach "refs/remotes/pr/$pr"
  fi
  url=$(url_of "$slot")

  # Own env, database, ports and compose project; same secrets as the agent's stack.
  sed -e "s#localhost:5432#localhost:$((5450 + slot))#g" "$REPO/.env" > "$d/.env"
  chmod 600 "$d/.env"
  sed -i -e '/^PUBLIC_BASE_URL=/d' -e '/^COMPOSE_/d' -e '/^AI_MONTHLY_BUDGET_USD=/d' "$d/.env"
  cat >> "$d/.env" <<EOF
PUBLIC_BASE_URL=$url/api
COMPOSE_PROJECT_NAME=elara-pr-$pr
COMPOSE_FILE=docker-compose.yml:compose.preview.yml
EOF
  cat > "$d/compose.preview.yml" <<EOF
# Preview of PR #$pr (written by preview.sh).
services:
  db:
    ports: !override
      - "127.0.0.1:$((5450 + slot)):5432"
  api:
    environment:
      ALLOWED_ORIGINS: $url
    ports: !override []
  web:
    ports: !override
      - "127.0.0.1:$((5190 + slot)):80"
EOF
  (cd "$d" && docker compose up -d --build --wait >/dev/null)
  (cd "$d" && set -a && . ./.env && set +a && make migrate >/dev/null && { $first && make seed >/dev/null || true; })
  tailscale serve --bg --https=$((8440 + slot)) "http://127.0.0.1:$((5190 + slot))" >/dev/null
  git -C "$d" rev-parse HEAD > "$d/.preview-head"
  echo "PR #$pr is up: $url"
}

down() {
  local pr="$1" d slot
  d=$(dir_of "$pr")
  [ -d "$d" ] || { echo "No preview for PR #$pr"; return 0; }
  slot=$(slot_of "$pr")
  [ -n "$slot" ] && tailscale serve --https=$((8440 + slot)) off >/dev/null 2>&1 || true
  (cd "$d" && docker compose down -v --remove-orphans >/dev/null 2>&1) || true
  git -C "$REPO" worktree remove --force "$d"
  echo "PR #$pr preview removed"
}

list() {
  local d pr
  for d in "$ROOT"/pr-*; do
    [ -d "$d" ] || continue
    pr=${d##*/pr-}
    echo "#$pr $(url_of "$(slot_of "$pr")")"
  done
}

sync() {
  local d pr state head
  for d in "$ROOT"/pr-*; do
    [ -d "$d" ] || continue
    pr=${d##*/pr-}
    state=$(gh pr view "$pr" -R "$(git -C "$REPO" remote get-url origin)" --json state -q .state 2>/dev/null || echo UNKNOWN)
    case "$state" in
      MERGED|CLOSED) down "$pr" ;;
      OPEN)
        head=$(gh pr view "$pr" -R "$(git -C "$REPO" remote get-url origin)" --json headRefOid -q .headRefOid)
        [ "$head" = "$(cat "$d/.preview-head" 2>/dev/null)" ] || up "$pr"
        ;;
    esac
  done
}

case "${1:-}" in
  up|down) [ -n "${2:-}" ] || { echo "usage: $0 $1 <pr>" >&2; exit 2; }; "$1" "${2#\#}" ;;
  list|sync) "$1" ;;
  *) sed -n '2,10p' "$0"; exit 2 ;;
esac
