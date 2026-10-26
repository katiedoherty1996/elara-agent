#!/usr/bin/env bash
# Put one plan into the Google Doc as its own tab (under "Plans"), with its mockup
# screenshots.
#
#   scripts/publish-plan.sh <task-id> "<tab title>" <plan.md>
#
# Every "![alt](path.png)" line in plan.md becomes that picture. Google Docs can only
# copy pictures from a public link, and the VPS has no public web server, so the PNGs go
# into a temporary SECRET gist (unlisted) for the few seconds Docs needs to copy them,
# and the gist is deleted straight after, whether publishing worked or not.
set -euo pipefail
AGENT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO="${ELARA_REPO:-$HOME/elara}"
task="$1" title="$2" plan="$(realpath "$3")"
set -a; [ -f "$REPO/.env" ] && . "$REPO/.env"; set +a

plan_dir="$(dirname "$plan")"
mapfile -t pics < <(grep -oE '^!\[[^]]*\]\([^)]+\.(png|jpg|jpeg)\)' "$plan" | sed -E 's/^.*\(([^)]+)\)$/\1/' | sort -u)

args=()
gist=""
work="$(mktemp -d)"
cleanup() {
  [ -n "$gist" ] && gh gist delete "$gist" --yes >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

if [ "${#pics[@]}" -gt 0 ]; then
  echo "Temporary image upload for an Elara plan; deleted automatically." >"$work/README.md"
  url=$(gh gist create -d "elara plan images (temporary)" "$work/README.md" | tail -1)
  gist="${url##*/}"
  git clone -q "https://gist.github.com/$gist.git" "$work/g"
  i=0
  for p in "${pics[@]}"; do
    src="$p"; [ -f "$src" ] || src="$plan_dir/$p"
    [ -f "$src" ] || { echo "publish-plan: image not found: $p" >&2; continue; }
    i=$((i + 1)); name="img$i.${src##*.}"
    cp "$src" "$work/g/$name"
    args+=(-image "$p=https://gist.githubusercontent.com/$(gh api user -q .login)/$gist/raw/$name")
  done
  git -C "$work/g" add . && git -C "$work/g" -c user.name=elara-agent -c user.email=elara-agent@users.noreply.github.com commit -qm images
  git -C "$work/g" push -q
fi

cd "$AGENT_DIR"
go run ./cmd/docplan publish -tasks "$REPO/.agent/tasks.json" -task "$task" -title "$title" -file "$plan" "${args[@]}"
