#!/usr/bin/env bash
# Pull the task Google Doc into <project>/.agent/tasks.json (and .agent/tasks-source.md).
# Needs GOOGLE_DOC_ID and GOOGLE_SA_KEY_FILE in the project's .env.
set -euo pipefail
AGENT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO="${ELARA_REPO:-$HOME/elara}"
set -a; [ -f "$REPO/.env" ] && . "$REPO/.env"; set +a
cd "$AGENT_DIR" && go run ./cmd/fetchtasks -tasks "$REPO/.agent/tasks.json" -out "$REPO/.agent/tasks-source.md"
