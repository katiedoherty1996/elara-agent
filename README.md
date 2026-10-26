# elara-agent

An unattended Claude Code build agent. It runs on a small Linux VPS, takes tasks from a
Google Doc, builds each one test-first on its own branch, reviews its own work for reuse,
security and performance, clicks through it like a QA tester, and opens a pull request
for a human to review and merge. It never merges and never pushes to `main`.

It was built to develop [Elara](https://github.com/katiedoherty1996/elara), an AI writing
app (Go API + React/TypeScript + PostgreSQL/pgvector), so the rulebook and subagents
talk about Elara. See [Adapting it to another project](#adapting-it-to-another-project).

## How the loop works

```
Google Doc ──fetchtasks──▶ .agent/tasks.json
                              │
                              ▼
        ┌──────── scripts/run-agent.sh (systemd, runs forever) ────────┐
        │ 1. sync tasks from the Doc                                   │
        │ 2. PR merged?            → mark its task done                │
        │ 3. new review comments?  → a session fixes them first        │
        │ 4. new task?             → ux-planner writes a plan into the │
        │                            Doc and waits for your Decision   │
        │ 5. approved task?        → a session builds it, opens a PR   │
        │ 6. usage limit / timeout → sleep, then resume the same session│
        │ 7. nothing to do         → check again in 5 minutes          │
        └──────────────────────────────────────────────────────────────┘
```

1. **Tasks doc.** You write tasks in a Google Doc:

   ```
   Task: Library screen
   Priority: high
   Design: library
   What: Show all of a user's posts with filter chips.
   Done when:
   - Posts list shows title, type, status
   - Filter chips filter the list
   ```

   `cmd/fetchtasks` reads the Doc with a read-only Google service account and merges it into
   `.agent/tasks.json`. Tasks already started keep their status; tasks deleted from the Doc
   are flagged, not dropped. See [`examples/tasks.json`](examples/tasks.json).
2. **Plan.** A new task first goes to the **ux-planner** subagent. It researches how other
   companies solve it and which tools could (popularity, ease, price), picks the simplest
   option for end users, draws a mockup from the design system, counts clicks and words,
   and writes the plan into its own tab under **Plans** in the Google Doc
   (`cmd/docplan`, `scripts/publish-plan.sh`). Change its `Decision:` line to `Approved`
   (or `Approved option B`) to have it built; write anything else to get a revised plan.
   Needs the Doc shared with the service account as **Editor**.
3. **Session.** Each loop starts one headless `claude -p` session, which follows
   [`CLAUDE.md`](CLAUDE.md) for one task: branch → design (if any) → schema → tests first →
   code → reuse / security / performance reviews → QA on the VPS → PR.
4. **PR.** The PR has a short plain-English description, screenshots (pushed to a
   `pr-assets` branch), and three report comments: 🔒 Security, 🧪 QA, ⚡ Performance.
5. **Review comments.** Comment on the PR. The runner picks up every comment that doesn't
   carry the agent's invisible `<!-- elara-agent -->` marker, and the next session turns each
   one into a `fix:` task on that branch, fixes it test-first, pushes and replies.
6. **Merge marks done.** When you merge a PR, the runner flips its task to `done`.

The agent's own state (`.agent/PROGRESS.md`, `tasks.json`, QA checklists, reports) stays on
the VPS and is never committed: PRs are code-only, enforced by a pre-push hook
([`lefthook-local.yml`](lefthook-local.yml)).

## What's in here

| Path | What it is |
|---|---|
| `scripts/run-agent.sh` | The loop: task sync, merge detection, review-comment pickup, session start/resume, usage-limit sleeps |
| `scripts/elara-agent.service` | systemd unit that keeps the loop running |
| `scripts/fetch-tasks.sh` | Pull the task Doc by hand |
| `cmd/docplan`, `internal/gdoc`, `scripts/publish-plan.sh` | Publish plans as Doc tabs and read back the Decision line |
| `cmd/fetchtasks`, `internal/tasks` | Go: read the Doc, parse `Task:` blocks, merge into `tasks.json` |
| `CLAUDE.md` | The project rulebook the agent follows (task flow, code style, PR format) |
| `rulebook/user-CLAUDE.md` | Owner rules that override everything else; goes in `~/.claude/CLAUDE.md` on the VPS |
| `.claude/agents/*.md` | Subagents: ux-planner (copy to `~/.claude/agents/`), design-implementer, postgres-schema, ai-rag-engineer, reuse-reviewer, security-reviewer, performance-tuner, qa-tester |
| `.claude/settings.json` | Permission deny-list (no force-push, no push to main, no merge, no reading `.env`/SSH keys) |
| `lefthook-local.yml` | Pre-push guard: PR branches may not touch `.agent/`, `.github/`, `docs/`, `design/` |
| `design/README.md` | How designs are handed to the agent |
| `examples/tasks.json` | Shape of the task file |

## Setup on a fresh Ubuntu VPS

Tested on Ubuntu 24.04, 4 vCPU / 8 GB.

1. **A user and the tools.** Create a non-root user with SSH key login only, then install
   `git`, `jq`, `gh`, Go, Node, Docker and [lefthook](https://github.com/evilmartians/lefthook),
   plus Claude Code (`curl -fsSL https://claude.ai/install.sh | bash`).
2. **Your project.** Clone the project the agent will build into `~/elara` (or set
   `ELARA_REPO`). Copy `CLAUDE.md`, `.claude/` and `lefthook-local.yml` into it, add
   `lefthook-local.yml` and `.agent/` to `.git/info/exclude`, and run `lefthook install`.
3. **This repo.** Clone it into `~/elara-agent`. Copy `rulebook/user-CLAUDE.md` to
   `~/.claude/CLAUDE.md`.
4. **GitHub.** `gh auth login` as the account that will open PRs.
5. **Claude subscription login** (see below): run `claude` once interactively and log in.
6. **Task Doc (optional).** Create a Google Cloud service account, download its JSON key to
   `~/.config/elara/gdocs-sa.json` (`chmod 600`), share the Doc with the service account's
   email as Viewer, and add to the project's `.env`:

   ```sh
   GOOGLE_SA_KEY_FILE=/home/YOUR_USER/.config/elara/gdocs-sa.json
   GOOGLE_DOC_ID=<the part of the Doc URL between /d/ and /edit>
   GOOGLE_DOC_TAB_ID=<optional: the t.xxxx from ?tab=t.xxxx>
   ```

   Without it the agent works from whatever is in `.agent/tasks.json`.
7. **Start it.**

   ```sh
   sed "s/YOUR_USER/$USER/g" ~/elara-agent/scripts/elara-agent.service \
     | sudo tee /etc/systemd/system/elara-agent.service
   sudo systemctl daemon-reload && sudo systemctl enable --now elara-agent
   tail -f ~/agent.log
   ```

## Run it on a Claude subscription, not an API key

The agent is meant to run on a Claude Pro/Max subscription login, so a long night of
building costs a flat fee instead of per-token API billing.

- Log in once with `claude` (interactive) as the service user. The login is stored in
  `~/.claude/.credentials.json`. **Never commit or copy that file anywhere public.**
- `run-agent.sh` starts every session with `env -u ANTHROPIC_API_KEY claude ...`. That
  matters: if `ANTHROPIC_API_KEY` is set, Claude Code uses it instead of the subscription
  login. The project's `.env` may well hold an API key for the *app's own* AI features, and
  the runner loads `.env`, so the key is stripped for the agent only.
- When the subscription hits a usage limit, the runner reads the reset time from Claude's
  final message, sleeps until then, and resumes the same session with `--continue`.

## Adapting it to another project

- **`CLAUDE.md`** is the part that's specific to Elara: the stack, code style, commands
  (`make up`, `make test`, ...), and the PR template. Rewrite it for your project and keep
  the working rules (never ask, never stop half-done, never push to main) and the task flow.
- **`.claude/agents/`**: keep `reuse-reviewer`, `security-reviewer`, `qa-tester` and
  `performance-tuner` and edit their specifics; drop `ai-rag-engineer` or `postgres-schema`
  if you don't need them.
- **`rulebook/user-CLAUDE.md`**: change the `pr-assets` screenshot URL to your repo.
- **`scripts/run-agent.sh`**: the prompts mention Elara and the comment marker
  `<!-- elara-agent -->`; rename them if you like (change the marker in `CLAUDE.md` too).
  `ELARA_REPO` and `ELARA_LOG` set where the project and log live.
- **The task format** is in `internal/tasks/tasks.go` (`Task:`, `Priority:`, `Design:`,
  `What:`, `Done when:`).

## Safety notes

- It runs with `--permission-mode auto`, so Claude Code's own safety checks still apply,
  plus the deny-list in `.claude/settings.json`.
- Keep secrets in `.env` (git-ignored) and out of the agent's reach where you can; the
  deny-list blocks reading `.env` directly.
- It only opens PRs. Review them; you are the one who merges.

## Licence

[MIT](LICENSE)
