# Elara — how to build here

Elara is an AI writing app: users chat with Elara to plan and draft blog posts and
newsletters, use a step-by-step **wizard** with contextual help, keep **source folders**
(uploaded documents used for retrieval), manage a **library** of drafts, **schedule**
posts on a calendar and **publish** them. Users belong to **organisations**; nobody may
ever see another organisation's data.

Stack: Go API (`backend/`), React + TypeScript (`frontend/`), PostgreSQL 18 + pgvector,
Docker Compose. You run unattended on a VPS. **Nobody is available to answer questions.**

## Working rules (non-negotiable)

1. **Never ask questions. Decide.** Pick the most reasonable option, write the assumption
   under the task in `.agent/PROGRESS.md`, add it to the task's "Looks dodgy" list in
   `.agent/qa-checklist.md`, and carry on.
2. **Never stop a task half-done and never leave it blocked.** If you are stuck, follow
   "When stuck" below until it is fixed. If something only Katie can supply is missing
   (design file, API key, external account) build around it (skip the design step, fake
   provider, stub behind an interface) and list it in the PR's "Worth a look".
3. **Running out of tokens is not finishing.** Code state lives in git, so always commit
   work-in-progress code to the task branch. Your own state lives in `.agent/` on this VM
   (never committed, see rule 6), so keep `PROGRESS.md` current. When resumed, read
   `PROGRESS.md` first and continue exactly where you left off.
4. **Never push to `main`, never merge a PR, never force-push.** Katie reviews and merges.
5. Secrets live only in `.env` (never committed). Never print them into logs, PRs or comments.
6. **PRs are code-only.** A PR's files must be product code (`backend/`, `frontend/`) and
   the build tooling it needs (Makefile, docker-compose.yml, .gitignore, .env.example, …).
   **Never commit, stage or push anything under `.agent/`, `.github/`, `docs/` or `design/`.**
   Keep writing those files where you always do (progress, tasks, checklists, reports,
   plans, perf write-ups, evals, API notes, design specs/refs/diffs); they stay on this VM
   only. Stage by path (`git add backend frontend …`), never `git add -f`. Before opening
   or updating a PR, `git diff --name-only origin/main...HEAD -- .agent .github docs design`
   must print nothing; if it does, restore those paths from `origin/main` and commit
   "chore: keep this PR code-only". A local pre-push guard enforces this; never bypass it
   (`--no-verify`, `LEFTHOOK=0`). Code comments must not point at files outside the repo
   (like `docs/api/x.md` or `.agent/reports/...`): put the detail in the comment itself.

## Task flow (fixed order)

Tasks come from Katie's Google Doc via `scripts/fetch-tasks.sh` → `.agent/tasks.json`.
Work the highest-priority `pending` task (`fix:` tasks first, then doc order).

1. **Start:** `git checkout main && git pull`, then `git checkout -b task/<id>-<short-name>`
   (branch from the unmerged dependency's branch instead if the task needs it; say
   "Builds on #N" in the PR). Set status `in_progress`. Add a heading for the task to
   `PROGRESS.md` and `qa-checklist.md`, and write the **happy path** into the checklist.
2. **Design first, if one exists.** Look in `design/manifest.json` / `design/` for this
   task's screens. Found → delegate to **design-implementer** before anything else.
   Not found → skip, build UI from existing tokens/components, and note
   "No design provided — styled to match existing screens" for the PR.
3. **Schema** changes → **postgres-schema**.
4. **AI chat / wizard / ingestion / retrieval** → **ai-rag-engineer**.
5. **Everything else, test-first (you):**
   - Write the failing tests (Go table-driven unit tests, integration tests against real
     Postgres, Vitest + React Testing Library for UI behaviour).
   - Run them and confirm they **fail for the right reason** (not a compile typo).
   - Write the minimum code to pass, then refactor for reuse.
6. **Reviews in order:** **reuse-reviewer** → **security-reviewer** (anything touching
   auth, endpoints, queries, uploads, AI) → **performance-tuner** (any new/changed query or
   list endpoint). Fix everything they find, test-first.
7. `make test` green → commit (conventional message) → push the branch.
8. **qa-tester last.** It deploys on this VM and works through the checklist. Its failures
   become `fix:` tasks on the same branch; fix them and let QA re-run.
9. **Open the PR** (template below) and post the three report comments.
   Set the task to `in_review`. It becomes `done` when Katie merges.

## When stuck — keep going, change approach every attempt

Log each attempt under "Unblocking <task>" in `PROGRESS.md`. Never repeat the same fix.
1. Read the whole error and logs; write a minimal failing test that reproduces it.
2. Re-read the rules in the relevant `.claude/agents/*.md`, then the links in `docs/references.md`.
3. Search official docs / the web for the exact error text.
4. Reset to the last green commit and redo the change in smaller steps.
5. Try a different design or library; record why.
6. Start a fresh session with only the problem summary.
Then loop back to 1 with what you learned. Keep going until it is fixed.

## QA checklist — keep it while you build

`.agent/qa-checklist.md`, under the task heading, three lists. Every item says **how** to
check it (UI steps, an API call, or SQL):
- **Looks dodgy** — anything you're not fully sure of: a guessed requirement, a race, a
  workaround, a TODO, code that "works but feels fragile". *If you think "that's probably
  fine", write it down here.*
- **Edge cases** — empty / max-length / unicode input, double-submit, session expiring
  mid-action, failed network, pagination edges, deleted-elsewhere items, another
  organisation's IDs, calendar timezone/date boundaries, big or wrong-type uploads.
- **Happy paths** — the user journey end to end.

## Code style — build like Katie

Katie's reference projects are the wedding site (Laravel backend, Vue/Quasar frontend).
Copy the *shape*, in Go and React:

**Backend (Go)**
- Layers: `handlers` (≈ Laravel controllers: decode → validate → call service → respond)
  → `services` (business rules, one struct per domain, e.g. `PostService`) →
  `repositories` (all SQL; every tenant query takes `orgID`) → `models` (domain structs).
  `cmd/api/main.go` only wires things together.
- Object-oriented Go: structs with methods, dependencies passed into constructors
  (`NewPostService(repo PostRepository, clock Clock)`), small interfaces defined where they
  are *used*, not where they are implemented.
- Validate at the top of every handler. Multi-step writes go in `database.WithTx`.
- Soft delete with `deleted_at` (Katie's `deletedDateTime` habit), `created_at`/`updated_at`.
- Helpers that more than one handler needs go in a purpose-named package (`httpx`,
  `auth`), never `util`/`common`.
- **Comments:** a doc comment on every exported name (full sentence starting with the
  name); a short comment above every method saying what it does; *why* comments on
  anything non-obvious — like Katie's docblocks ("POST not PUT so multipart works").

**Go standards**
- `gofmt` + `goimports`; imports grouped std / third-party / local.
- Package names short, lowercase, single word. One-method interfaces end in `-er`.
- Handle every error; wrap with `fmt.Errorf("doing x: %w", err)`; return early; error is
  the last return value. No panics in request paths.
- Pointer receivers when a method changes state; an options struct when there are many params.
- Pass `context.Context` first; every goroutine has a clear exit; no leaks.
- Table-driven tests in `_test.go` with clear failure messages.
- Preallocate slices you know the size of; `strings.Builder` for building strings;
  measure (pprof / EXPLAIN) before optimising.

**Frontend (React + TypeScript)**
- `src/api/client.ts` — the one HTTP client (≈ `boot/axios.js`): base URL, credentials,
  CSRF header, 401 → clear session and go to login.
- `src/services/` — `createResourceService<T>(path)` gives list/get/create/update/remove;
  each resource file is a few lines on top of it. Components never call `fetch` directly.
- `src/hooks/` — reusable logic (`useApi`, `useAsync`, `usePagination`, `useForm`,
  `useChatStream`, formatters) like `usePriceFormatter`.
- `src/components/` — generic, config-driven components (like `SelectDropdown`, which
  takes options + label/value keys). Check here before creating anything new.
- `src/pages/` — one per screen. Screens that differ only by data share one page via route
  props (like `ProductGallery` + `productTypeVariable`).
- `src/router/routes.tsx` — route table with `meta.requiresAuth`.
- Styling: CSS modules + `src/styles/tokens.css` only. No inline style strings.
- A short block comment at the top of every component and hook.

## Reuse — the most important rule

Katie does not want lots of code where a reusable piece would do.
- **Search before you write**: `rg` through `httpx/`, `services/`, `repositories/`,
  `components/`, `hooks/`, `lib/` for something that already does it.
- The **second** time logic appears, extract it. No copy-pasted block over ~5 lines.
- Backend: generic `httpx.Respond/Decode/Paginate`; generic repository helpers for
  find-by-org / list-by-org / soft-delete. Frontend: the resource factory, generic
  components, shared hooks.
- `reuse-reviewer` must pass before every PR.

## AI features are tested for real

Fakes keep unit tests fast, but every AI task must also pass the live tests against the real
Claude + Voyage APIs (`go test -tags live`) and QA must use the real AI in the browser. See
`.claude/agents/ai-rag-engineer.md` §11. No keys yet → say so in the PR and re-test once they exist.

## Security basics (details in `.claude/agents/security-reviewer.md`)

Deny by default. Every request is authenticated and authorised in middleware. The
organisation comes from the server-verified membership, never from the client. Every
tenant query filters by `organisation_id`, and Postgres RLS backs it up. Another
organisation's object returns **404**. Parameterised SQL only. Explicit input structs
(no mass assignment).

## Pull requests

`gh pr create --base main` with this body. Keep it short and plain, with no jargon:

```markdown
## What this adds
One or two plain sentences, from the user's point of view.

## Screenshot
![<screen>](https://github.com/<owner>/elara/blob/pr-assets/<task-id>.png?raw=true)

## How it was checked
- ✅ Tests pass (N new)
- ✅ Matches the design   (or: No design provided — styled to match existing screens)
- ✅ Security check: other organisations can't see this data
- ✅ QA: happy path + N edge cases clicked through

## Worth a look
- One line per assumption / dodgy item / missing key. Or "Nothing".
```

End **every** PR description and every comment you post with `<!-- elara-agent -->`
(invisible on GitHub). The runner uses it to tell your comments apart from Katie's review
comments, because both are posted from the same GitHub account.

Screenshots come from qa-tester (max 3), pushed to the `pr-assets` branch, never to the
task branch. That branch only holds screenshots and never gets a PR:

```sh
git fetch origin pr-assets
git worktree add -B pr-assets /tmp/pr-assets origin/pr-assets
cp <shot>.png /tmp/pr-assets/<task-id>[-n].png
git -C /tmp/pr-assets add . && git -C /tmp/pr-assets commit -m "chore: screenshots for <task-id>"
git -C /tmp/pr-assets push origin pr-assets && git worktree remove /tmp/pr-assets
```

For a backend-only task with no UI, put a short request/response example in place of the image.

Then post **three separate comments** with `gh pr comment`, built from the report files
(never from memory): `.agent/reports/<id>/security.md`, `.agent/qa-reports/<id>.md`,
`.agent/reports/<id>/performance.md`.

```markdown
## 🔒 Security        (or 🧪 QA / ⚡ Performance)
**Checked and passed**
- ✅ plain-English line per check
**Found and fixed**
- 🔧 what was wrong → how it was fixed   (or "Nothing found")
```
If a section doesn't apply, its comment still gets posted and says why (e.g. "Not relevant
to this change — no new queries"). When later fixes change a result, edit the existing
comment (`gh api -X PATCH repos/{owner}/{repo}/issues/comments/{id}`) instead of adding another.

If Katie leaves review comments, turn each one into a `fix:` task on that branch, fix it
test-first, push, and reply on the PR in one line.

## Commands

- `make up` / `make down`: start or stop the stack (postgres, api, web)
- `make test`: run every test (backend unit + integration, frontend)
- `make lint`: golangci-lint, eslint, tsc
- `make migrate`, `make migrate-down`: run goose migrations
- `make seed`: load two demo organisations
- `cd frontend && npx playwright test`: e2e and visual tests

## Files you maintain (on this VM only, never committed)

- `.agent/tasks.json`: task id, title, status (`pending | in_progress | in_review | done`), branch, pr, notes
- `.agent/PROGRESS.md`: a running log. Read it first after every resume.
- `.agent/qa-checklist.md`: the per-task QA lists above
- `.agent/reports/<id>/` and `.agent/qa-reports/<id>.md`: the reviewer outputs

Rules here are distilled from Katie's reference links. When a rule isn't clear enough for
the situation, read the matching link in `docs/references.md`.
