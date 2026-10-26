# Rules for the Elara build agent (from Katie — these override CLAUDE.md, .claude/agents/*.md and the runner prompt)

## PRs are code-only

A PR's "Files changed" must contain **only product code**: `backend/`, `frontend/` and the
build tooling next to them (Makefile, docker-compose.yml, lefthook.yml, .gitignore,
.env.example, scripts/ when the change needs it).

**Never commit, stage or push anything under `.agent/`, `.github/`, `docs/` or `design/`
on any branch.** Keep writing those files where you always do — they stay on this VM as
local files only. This covers PROGRESS.md, tasks.json, qa-checklist.md, reports,
qa-reports, plans (`docs/plans/`), perf write-ups (`docs/perf/`), evals (`docs/evals/`),
API notes (`docs/api/`), design specs/refs/diffs (`design/<screen>/...`), workflows and
the PR template.

- Where CLAUDE.md or the runner prompt says "commit `.agent/...` on the branch" or "commit
  work in progress", commit only the code; just save the `.agent/` files.
- Stage files by path (`git add backend frontend ...`). Never `git add` those four folders,
  never `git add -f`, never `git update-index --no-skip-worktree`, and never edit the
  "Katie rule" lines in `.git/info/exclude`.
- Before opening or updating a PR, check that
  `git diff --name-only origin/main...HEAD -- .agent .github docs design` prints nothing.
  If it prints files, restore them (`git checkout origin/main -- <path>`, or
  `git rm --cached` for new files), commit "chore: keep this PR code-only", push.
- A pre-push guard (`lefthook-local.yml`) rejects pushes that break this rule. Fix the
  branch as above; never bypass it with `--no-verify` or `LEFTHOOK=0`.
- Code comments must not point at files that are not in the repo (e.g. `docs/api/x.md`,
  `.agent/reports/...`). Put the needed detail in the comment itself.

## PR screenshots go on the `pr-assets` branch

Not in `.github/pr-assets/`. To add one:

    git fetch origin pr-assets
    git worktree add -B pr-assets /tmp/pr-assets origin/pr-assets
    cp <shot>.png /tmp/pr-assets/<task-id>[-n].png
    git -C /tmp/pr-assets add . && git -C /tmp/pr-assets commit -m "chore: screenshots for <task-id>"
    git -C /tmp/pr-assets push origin pr-assets
    git worktree remove /tmp/pr-assets

Link them in the PR as
`![<screen>](https://github.com/katiedoherty1996/elara/blob/pr-assets/<task-id>.png?raw=true)`.
The `pr-assets` branch never gets a PR.
