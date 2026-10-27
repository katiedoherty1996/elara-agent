#!/usr/bin/env bash
# Elara's unattended build loop. Runs forever (under systemd: elara-agent.service).
#
# Each loop:
#   1. sync tasks from the Google Doc
#   2. mark tasks done when Katie has merged their PR
#   3. collect new PR review comments from Katie
#   4. if there is work, run one Claude Code session (one task, or the review fixes)
#   5. if Claude hit a usage limit, sleep until it resets, then resume the same session
#
# Running out of tokens never finishes a task: the session is resumed with --continue.
set -uo pipefail

REPO="${ELARA_REPO:-$HOME/elara}"                  # the project the agent builds
AGENT_DIR="$(cd "$(dirname "$0")/.." && pwd)"     # this repo (fetchtasks lives here)
LOG="${ELARA_LOG:-$HOME/agent.log}"
RUNNER="$REPO/.agent/runner"
IDLE_SLEEP=300          # nothing to do: check again in 5 minutes
TASK_TIMEOUT=4h         # one session's wall-clock cap; the next loop resumes it
AGENT_MARKER="<!-- elara-agent -->"   # every comment the agent posts carries this

export PATH="$HOME/.local/bin:/usr/local/go/bin:$HOME/go/bin:$PATH"
cd "$REPO" || exit 1
mkdir -p "$RUNNER"
touch "$RUNNER/handled-comments.txt"

log() { echo "$(date -Is) $*" | tee -a "$LOG"; }

# load_env exports .env so make, go run and gh see the same settings.
load_env() { set -a; [ -f .env ] && . ./.env; set +a; }

# sync_tasks pulls the Google Doc into .agent/tasks.json. A failure is logged and the
# loop carries on with the existing task list.
sync_tasks() {
  if [ -z "${GOOGLE_DOC_ID:-}" ] || [ ! -f "${GOOGLE_SA_KEY_FILE:-/nonexistent}" ]; then
    log "tasks: Google Doc not configured yet; using existing tasks.json"
    return
  fi
  (cd "$AGENT_DIR" && go run ./cmd/fetchtasks -tasks "$REPO/.agent/tasks.json" -out "$REPO/.agent/tasks-source.md") >>"$LOG" 2>&1 \
    || log "tasks: fetch failed (see above); using existing tasks.json"
}

# sync_plan_decisions reads Katie's Decision line on each plan tab in the Doc:
# approved plans go back to pending (ready to build), others to plan_changes.
sync_plan_decisions() {
  if [ -z "${GOOGLE_DOC_ID:-}" ] || [ ! -f "${GOOGLE_SA_KEY_FILE:-/nonexistent}" ]; then return; fi
  (cd "$AGENT_DIR" && go run ./cmd/docplan decisions -tasks "$REPO/.agent/tasks.json") 2>&1 | tee -a "$LOG" >/dev/null \
    || log "plans: reading decisions failed (see above)"
}

# mark_merged flips in_review tasks to done once their PR is merged.
mark_merged() {
  local pr state tmp
  for pr in $(jq -r '.tasks[] | select(.status=="in_review" and (.pr // 0) > 0) | .pr' .agent/tasks.json); do
    state=$(gh pr view "$pr" --json state -q .state 2>/dev/null || echo unknown)
    if [ "$state" = "MERGED" ]; then
      tmp=$(mktemp)
      jq --argjson pr "$pr" '(.tasks[] | select(.pr == $pr)).status = "done"' .agent/tasks.json >"$tmp" && mv "$tmp" .agent/tasks.json
      log "tasks: PR #$pr merged → done"
    fi
  done
}

# collect_review_comments writes Katie's new PR comments (anything without the agent
# marker, not yet handled) to new-comments.md. Returns 0 if there are any.
collect_review_comments() {
  local out="$RUNNER/new-comments.md" repo pr
  : >"$out"
  repo=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null) || return 1
  for pr in $(gh pr list --state open --json number -q '.[].number' 2>/dev/null); do
    {
      gh api "repos/$repo/issues/$pr/comments" --paginate \
        --jq ".[] | select(.body | contains(\"$AGENT_MARKER\") | not) | \"\(.id)\t$pr\tconversation\t\(.body | gsub(\"\n\"; \" \"))\""
      gh api "repos/$repo/pulls/$pr/comments" --paginate \
        --jq ".[] | select(.body | contains(\"$AGENT_MARKER\") | not) | \"\(.id)\t$pr\t\(.path):\(.line // .original_line)\t\(.body | gsub(\"\n\"; \" \"))\""
      gh api "repos/$repo/pulls/$pr/reviews" --paginate \
        --jq ".[] | select(.body != \"\" and (.body | contains(\"$AGENT_MARKER\") | not)) | \"\(.id)\t$pr\treview (\(.state))\t\(.body | gsub(\"\n\"; \" \"))\""
    } 2>/dev/null | while IFS=$'\t' read -r id prnum where body; do
      grep -qx "$id" "$RUNNER/handled-comments.txt" && continue
      printf -- "- comment %s on PR #%s (%s): %s\n" "$id" "$prnum" "$where" "$body" >>"$out"
    done
  done
  [ -s "$out" ]
}

# ai_budget_guard adds up this month's real-AI test spend (.agent/ai-spend.tsv, written
# by the agent) and, once it reaches AI_TEST_BUDGET_USD, forces the fake AI for the
# session: docker compose prefers the shell's LLM_PROVIDER over .env.
ai_budget_guard() {
  local budget="${AI_TEST_BUDGET_USD:-23}" spent
  spent=$(awk -F'\t' -v m="$(date +%Y-%m)" 'NR>1 && index($1,m)==1 {s+=$4} END {printf "%.2f", s+0}' .agent/ai-spend.tsv 2>/dev/null || echo 0)
  if awk -v s="$spent" -v b="$budget" 'BEGIN {exit !(s >= b)}'; then
    export LLM_PROVIDER=fake AI_BUDGET_USED_UP=1
    log "ai budget: \$$spent of \$$budget used this month; real AI off for testing"
  else
    unset AI_BUDGET_USED_UP
    [ "${LLM_PROVIDER:-}" = fake ] && unset LLM_PROVIDER
  fi
}

# has_work reports whether any task still needs the agent.
has_work() {
  if planning_paused; then
    # Only tasks that are ready to build count; unplanned Doc tasks wait.
    [ "$(jq '[.tasks[] | select((.status=="in_progress" or (.status=="pending" and (((.fromDoc | not) and (.needsPlan | not)) or .planApproved))) and (.removedInDoc | not))] | length' .agent/tasks.json)" -gt 0 ]
  else
    [ "$(jq '[.tasks[] | select((.status=="pending" or .status=="in_progress" or .status=="plan_changes") and (.removedInDoc | not))] | length' .agent/tasks.json)" -gt 0 ]
  fi
}

# planning_paused is true while Katie is still editing the Doc: touch
# .agent/runner/planning-paused to stop new plans, delete it to start again.
planning_paused() {
  [ -f "$RUNNER/planning-paused" ]
}

# final_message prints Claude's closing result text plus any non-JSON lines (stderr).
# Only this is checked for usage limits — never the whole transcript, which may contain
# words like "rate limit" from the work itself (e.g. login throttling).
final_message() {
  local file="$1"
  jq -r 'select(.type=="result") | "\(.is_error) \(.result // "")"' "$file" 2>/dev/null | tail -1
  grep -v '^{' "$file" | tail -20
}

# limit_reset_epoch prints the epoch second a usage limit resets, "unknown" if Claude
# hit a limit without saying when, or nothing if this wasn't a usage limit.
limit_reset_epoch() {
  local file="$1" rc="$2" tail_txt epoch when
  tail_txt=$(final_message "$file")
  # A clean, successful run is never a usage limit.
  if [ "$rc" -eq 0 ] && ! echo "$tail_txt" | grep -q '^true '; then return 0; fi
  echo "$tail_txt" | grep -qiE 'usage limit|limit reached|rate.?limit|resets? (at )?[0-9]|weekly limit|5-hour limit|overloaded|429' || return 0
  # Format 1: "...limit reached|1767225600"
  epoch=$(echo "$tail_txt" | grep -oE 'limit reached\|[0-9]{10}' | tail -1 | grep -oE '[0-9]{10}')
  if [ -n "$epoch" ]; then echo "$epoch"; return 0; fi
  # Format 2: "resets 3pm" / "resets at 3:15pm (Europe/Dublin)" / "resets Oct 6, 9am"
  when=$(echo "$tail_txt" | grep -oiE 'resets? (at )?[A-Za-z]{0,3} ?[0-9]{0,2},? ?[0-9]{1,2}(:[0-9]{2})? ?(am|pm)?' | tail -1 | sed -E 's/^[Rr]esets? (at )?//' | tr -d ,)
  if [ -n "$when" ]; then
    epoch=$(date -d "$when" +%s 2>/dev/null) || epoch=""
    # A bare time earlier than now means tomorrow.
    if [ -n "$epoch" ] && [ "$epoch" -lt "$(date +%s)" ]; then epoch=$((epoch + 86400)); fi
    if [ -n "$epoch" ]; then echo "$epoch"; return 0; fi
  fi
  echo "unknown"
}

PROMPT_TEMPLATE='You are the Elara build agent running unattended on the VPS. Nobody can answer questions — decide and log assumptions.
Follow CLAUDE.md exactly.
1. Read .agent/PROGRESS.md.
2. If .agent/runner/new-comments.md has entries, handle those first: each is Katie'"'"'s PR review comment. Make each one a fix: task on that PR'"'"'s branch, fix it test-first, push, reply on the PR in one plain line (include the marker <!-- elara-agent --> in every comment you post), then append each handled comment id on its own line to .agent/runner/handled-comments.txt.
@@STEP3@@
4. Otherwise take the next task to build from .agent/tasks.json (fix: tasks first, then in_progress, then pending with planApproved, in doc order; skip removedInDoc, plan_review, and needsPlan tasks without planApproved) and take it through the whole flow until its PR is open with the three report comments. Build the option Katie approved (planChoice) from .agent/plans/<id>.md.
You are in auto mode and nobody can answer: never ask, answer any question yourself with good judgement, and note each such decision (see "Auto mode" in ~/.claude/CLAUDE.md).
When you mean a plan, use its tab link (planTabUrl in .agent/tasks.json).
Put <!-- elara-agent --> at the end of every PR description and comment you write.
PRs are code-only: never commit .agent/, .github/, docs/ or design/ (CLAUDE.md rule 6).
Finish that one piece of work, update .agent/tasks.json and .agent/PROGRESS.md (save them, do not commit them), then stop.'

STEP3_PLAN='3. Otherwise, if a task is plan_changes, or a pending task with fromDoc true or needsPlan true has no planApproved, plan it first (plan_changes first, then doc order): delegate that ONE task to the ux-planner subagent. It researches, draws a mockup, publishes the plan to its own tab in the Google Doc and sets the task to plan_review. Build nothing for it; stop after that one plan.'
STEP3_PAUSED='3. Planning is paused: Katie is still writing the Google Doc. Do not plan any task and do not use the ux-planner subagent. Skip pending tasks with fromDoc true or needsPlan true and no planApproved, and plan_changes tasks.'

# prompt_new fills in step 3 depending on whether planning is paused.
prompt_new() {
  if [ -n "${AI_BUDGET_USED_UP:-}" ]; then
    echo "This month's AI test budget is used up: do not call the real Claude API (LLM_PROVIDER=fake is set); say so in the PR."
  fi
  if planning_paused; then
    echo "${PROMPT_TEMPLATE/@@STEP3@@/$STEP3_PAUSED}"
  else
    echo "${PROMPT_TEMPLATE/@@STEP3@@/$STEP3_PLAN}"
  fi
}

PROMPT_RESUME='Continue exactly where you left off (you were interrupted by a usage limit or timeout). Re-read .agent/PROGRESS.md and the current git state first. Do not consider the task finished until its PR is open with the three report comments.'

backoff=900
log "runner: started (repo $REPO)"

while true; do
  load_env
  git fetch -q origin 2>>"$LOG" || log "git: fetch failed"
  sync_tasks
  sync_plan_decisions
  mark_merged
  ai_budget_guard
  # PR previews on Tailscale: drop merged/closed ones, rebuild ones with new commits.
  "$AGENT_DIR/scripts/preview.sh" sync >>"$LOG" 2>&1 || log "preview: sync failed"

  resume=false
  [ -f "$RUNNER/resume" ] && resume=true

  if ! $resume; then
    if collect_review_comments; then
      log "runner: $(wc -l <"$RUNNER/new-comments.md") new review comment(s) to handle"
    elif ! has_work; then
      log "runner: idle (no pending tasks, no new review comments)"
      sleep "$IDLE_SLEEP"
      continue
    fi
  fi

  if $resume; then
    log "runner: resuming previous session"
    args=(--continue -p "$PROMPT_RESUME")
  else
    log "runner: starting a new session"
    args=(-p "$(prompt_new)")
  fi

  out="$RUNNER/session-$(date +%Y%m%d-%H%M%S).jsonl"
  # .env holds the app's ANTHROPIC_API_KEY (AI wizard + RAG testing only). Hide it so the
  # agent always runs on the Claude subscription login and never bills the API account.
  timeout "$TASK_TIMEOUT" env -u ANTHROPIC_API_KEY claude "${args[@]}" --permission-mode auto --disallowedTools AskUserQuestion --output-format stream-json --verbose >"$out" 2>&1
  rc=$?

  reset=$(limit_reset_epoch "$out" "$rc")
  if [ -n "$reset" ]; then
    touch "$RUNNER/resume"
    if [ "$reset" = "unknown" ]; then
      log "runner: usage limit hit, reset time unknown; sleeping $((backoff / 60)) min"
      sleep "$backoff"
      backoff=$((backoff * 2 > 3600 ? 3600 : backoff * 2))
    else
      wait_s=$((reset - $(date +%s) + 120))
      [ "$wait_s" -lt 60 ] && wait_s=60
      log "runner: usage limit hit; sleeping until $(date -d "@$((reset + 120))" '+%a %H:%M') ($((wait_s / 60)) min)"
      sleep "$wait_s"
    fi
    continue
  fi
  backoff=900

  if [ "$rc" -eq 124 ]; then
    log "runner: session hit the ${TASK_TIMEOUT} cap; will resume it"
    touch "$RUNNER/resume"
    continue
  fi
  if [ "$rc" -ne 0 ]; then
    log "runner: claude exited with $rc (see $out); resuming after 60s"
    touch "$RUNNER/resume"
    sleep 60
    continue
  fi

  rm -f "$RUNNER/resume"
  log "runner: session finished cleanly"
  # Keep the last 50 session logs.
  ls -1t "$RUNNER"/session-*.jsonl 2>/dev/null | tail -n +51 | xargs -r rm -f
  sleep 10
done
