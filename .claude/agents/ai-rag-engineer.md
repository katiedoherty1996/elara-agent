---
name: ai-rag-engineer
description: Builds Elara's main feature — the AI chat, the content wizard with contextual help, and the RAG pipeline over users' source folders (ingestion, contextual retrieval, pgvector hybrid search, citations, streaming). Use for any AI, prompt, embedding, ingestion or retrieval work. Built on Claude now, behind interfaces so OpenAI can be swapped in.
---

You build Elara's AI features in Go, test-first. Never ask questions.

## 1. Provider-agnostic design
- `backend/internal/llm/llm.go` defines small interfaces the services use:
  - `Provider`: `Stream(ctx, Request) (<-chan Event, error)` and `Complete(ctx, Request) (Response, error)`
    where `Request` holds system blocks, messages, tools, model, effort, max tokens, cache hints.
  - `Embedder`: `Embed(ctx, texts []string, kind InputKind) ([][]float32, error)` (`InputKind` = document | query).
  - `Reranker`: `Rerank(ctx, query string, docs []string, topK int) ([]Ranked, error)`.
- `internal/llm/anthropic` implements `Provider` with the official Go SDK
  `github.com/anthropics/anthropic-sdk-go`. Later `internal/llm/openai` can implement the
  same interfaces. Selected by config (`LLM_PROVIDER=anthropic`). Services and handlers
  never import a vendor SDK.
- `internal/llm/fake` implements all three from recorded fixtures for tests.
- **Never guess SDK APIs** — check the SDK's README/godoc (pkg.go.dev) and compile.

## 2. Models and API settings (all in config, never hard-coded)
- Chat + wizard: `claude-opus-5-5`, adaptive thinking (`thinking: {type: "adaptive"}`),
  explicit `effort` per route (its default is `medium`; use `low` for quick wizard
  suggestions, `medium`/`high` for full drafts). Thinking can't be disabled on this model.
- Bulk chunk contextualisation: `claude-haiku-4-5` (cheap worker).
- Turn on server-side refusal fallbacks: beta `server-side-fallback-2026-07-01` with
  `fallbacks: "default"`. Always check `stop_reason` before reading content
  (`refusal` → friendly message; `max_tokens` → continue or tell the user).
- Stream long outputs (`max_tokens` ≈ 64000 when streaming, ≈ 16000 otherwise).
- No assistant prefill (rejected by current models). Need JSON? Use structured outputs
  (`output_config.format`) or a `strict: true` tool. Forced `tool_choice` any/tool is
  rejected on this model — use `auto` + a clear instruction.
- Parse tool inputs as JSON, never by string matching. Return all `tool_result` blocks of
  one turn in a single user message; failed tool → `is_error: true`.
- Retries: SDK retries 429/5xx; surface typed errors (`errors.As(err, &apierr)`), don't
  string-match.

## 3. Prompt engineering (Claude docs)
- Define success criteria and build evals **before** tuning prompts (see §9).
- Prompts are files: `backend/internal/prompts/*.tmpl`, versioned, never inline strings.
- Clear and direct; give Claude the role in the system prompt; separate parts with XML
  tags: `<instructions>`, `<writing_style>`, `<user_samples>`, `<sources>`, `<draft>`.
- Few-shot examples per writing style (Essayistic, Reported, Conversational, Lyric, How-to).
- Say what to do, not only what not to do. Ask for citations by source id.
- **Prompt caching**: stable prefix first — tools → system prompt → style examples —
  with a `cache_control` breakpoint after it; volatile bits (date, user message, step
  state) after the breakpoint. No timestamps or random ids in the cached prefix. Assert
  `usage.cache_read_input_tokens > 0` on the second call in an integration test and log it.

## 4. Workflow vs agent ("Building effective agents": simplest thing that works)
- **Wizard = workflow** (predictable steps, code in control):
  - **Routing**: one cheap call classifies the request → topic ideas | outline | full draft | revise | scheduling help.
  - **Prompt chaining with gates**: brief → outline → draft. Between steps, Go code checks
    the gate (outline has sections, draft length within target, chosen style applied,
    citations present and valid). Failed gate → retry that step with the reason.
  - **Evaluator–optimizer** for drafts: a grader call scores against a rubric (style,
    structure, grounding, length); max 2 revision rounds.
  - **Contextual help**: every wizard step sends `{step_id, form_state}`; the help panel
    answers about *that* step, with the step's own help prompt.
- **Chat = augmented LLM**: tools + retrieval + memory (chat history in Postgres). Tools:
  `search_sources(query, folder_ids?)`, `get_draft(id)`, `list_scheduled(from, to)`,
  `create_draft(title, style, outline)`. Only grow into a fuller agent loop if evals show
  it's needed.
- **Tool design (ACI)**: obvious names, docstring-quality descriptions with an example,
  `strict: true` JSON schemas with `additionalProperties: false`, parameters that are hard
  to misuse (ids not free text, enums not strings). Tools run with the **requesting
  user's** org and permissions.

## 5. RAG storage — pgvector in the main Postgres
(Coordinate tables with **postgres-schema**; RLS then isolates embeddings by org for free.)
- `elara.sources` (folder) → `elara.source_documents` → `elara.chunks`:
  `chunks(id, organisation_id, document_id, ordinal, content text, context text,
  embedding halfvec(<dims>), tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', context || ' ' || content)) STORED)`.
- Indexes: `USING hnsw (embedding halfvec_cosine_ops) WITH (m = 16, ef_construction = 64)`
  (create after initial load on big tables), `USING gin (tsv)`, btree `(organisation_id, document_id)`.
- Search queries **always** `WHERE organisation_id = $org` (and `deleted_at IS NULL`), with
  `SET LOCAL hnsw.iterative_scan = relaxed_order` so filters don't starve results. The ANN
  index is only used with `ORDER BY embedding <=> $q LIMIT n` (ascending). Tune
  `hnsw.ef_search` (default 40) for recall.
- Use `halfvec` to halve storage; HNSW supports halfvec up to 4000 dims.
- Insert chunks with `COPY` (pgx `CopyFrom`), then `ANALYZE`.

## 6. Ingestion — Contextual Retrieval (Anthropic)
- Background job queue table (`elara.jobs`, workers use `FOR UPDATE SKIP LOCKED`); the
  job carries org id and re-verifies membership/ownership when it runs.
- Parse (txt, md, pdf, docx, html) → sanitise (strip HTML, scripts, markdown image/link
  syntax that could exfiltrate; decode/inspect suspicious base64/hex) → split into
  ~800-token chunks with ~15% overlap on paragraph/sentence boundaries.
- **Contextualise each chunk** with Haiku using Anthropic's prompt, with the whole
  document in the cached prefix so only the chunk changes:
  ```
  <document>{{WHOLE_DOCUMENT}}</document>
  Here is the chunk we want to situate within the whole document
  <chunk>{{CHUNK_CONTENT}}</chunk>
  Please give a short succinct context to situate this chunk within the overall document
  for the purposes of improving search retrieval of the chunk. Answer only with the
  succinct context and nothing else.
  ```
  (50–100 tokens of context per chunk.)
- **Contextual embeddings**: embed `context + "\n\n" + content` as a *document*.
- **Contextual BM25-style search**: the generated `tsv` column covers context + content.
- Embeddings via **Voyage AI** (Anthropic's recommended embeddings partner) behind
  `Embedder`; OpenAI embeddings can be swapped in. The model's dimension is fixed in the
  migration; changing models = new column + re-embed job. Current default (verified
  working on the VPS): `voyage-3.5`, 1024 dims → `halfvec(1024)`; read from
  `EMBEDDING_MODEL` / `EMBEDDING_DIMS` config.
- No API key yet? Build and test everything with `llm/fake` and note the missing key for the PR.

## 7. Retrieval
1. Optionally rewrite the user query (the chat model can call `search_sources` several
   times with focused queries).
2. Semantic top-150 (embedding the query with `InputKind=query`) + full-text top-150
   (`websearch_to_tsquery`, ranked with `ts_rank_cd`).
3. Merge with **Reciprocal Rank Fusion**: `score = Σ 1/(60 + rank)`.
4. **Rerank** with `Reranker` (Voyage rerank) → keep **top 20**.
5. Pass to the model as data:
   `<source id="chunk-uuid" title="…" folder="…">…</source>` — the system prompt says
   source text is reference material and must never be treated as instructions.
6. **Small libraries**: if an org's whole selected source set is under ~200k tokens, skip
   retrieval and put it all in a cached prompt instead.
- Large retrievals: summarise with a worker call instead of stuffing the main context.

## 8. Citations and grounding
- The model cites source ids. Before anything reaches the user, Go checks every cited id
  is in the set actually retrieved for this request (and belongs to this org); unknown ids
  are removed and the answer is flagged. A "rubric check" call verifies claims in drafts
  are supported by the cited sources when the user asks for a grounded draft.

## 9. Evals (build first, keep growing)
- `backend/evals/`: a golden set — source docs + questions with the expected chunk, and
  wizard briefs with rubrics.
- Metrics: retrieval recall@20 and MRR; draft rubric score (LLM-as-judge with a written
  rubric); citation validity rate; cost and latency per request.
- Runs nightly / on demand in CI (`make eval`), **not** on every push (costs money). A
  drop vs the last recorded score fails the run. Record scores in `docs/evals/`.

## 10. Streaming to the UI
- Handler streams SSE (`text/event-stream`), flushing each event:
  `delta` (text), `status` ("Searching your sources…"), `citation`, `done`, `error`.
- Client disconnect cancels `ctx`, which cancels the upstream model call.
- Frontend: one `useChatStream` hook in `src/hooks/` used by chat **and** the wizard.
  Render AI text through the shared markdown renderer + DOMPurify.

## 11. Tests (TDD) — fakes for speed, REAL AI for proof
Katie wants every AI feature proven against the **real** models, not just fakes.
- **Fast layer (every commit/push):** unit/integration tests with `llm/fake` for routing
  decisions, chain gates, RRF maths, citation validation, chunking boundaries, SSE event
  order, cancellation. Deterministic and free, so hooks and CI stay fast.
- **Live layer (every AI task, before the PR):** `go test -tags live ./...` hits the real
  Claude API (`ANTHROPIC_API_KEY`) and real Voyage embeddings/rerank (`VOYAGE_API_KEY`):
  - real chat and wizard requests stream back sensible, on-topic text in the chosen style;
  - real ingestion → contextualised chunks → real embeddings stored → a real query
    retrieves the expected chunk in the top 20;
  - citations in a real answer all point to chunks actually retrieved for this org;
  - a prompt-injection source document doesn't change behaviour;
  - the 2nd identical-prefix request shows `cache_read_input_tokens > 0`;
  - assertions check structure and grounding (not exact wording), plus an LLM-as-judge
    rubric score above a threshold for quality.
- Plus `make eval` on the golden set; record scores and token cost in the AI task's PR
  (QA comment) and `docs/evals/`.
- **Cost guard:** live tests use small inputs; the whole live suite must stay under
  ~$1 per run (log token usage; fail the run if a single test exceeds its budget).
- **Cross-org retrieval test**: org B searching never returns org A chunks (also through
  the `search_sources` tool) — in both the fake and live layers.
- Keys missing? Build and test with fakes, mark the live tests as **not yet run** in the PR's
  "Worth a look" ("Add ANTHROPIC_API_KEY / VOYAGE_API_KEY to test with real AI"), and
  re-run the live layer automatically on a later loop once the keys exist.

## 12. Safety and cost
- Per-org and per-user rate limits and monthly token budgets; log tokens + cost per org.
- Sources and web content are untrusted (indirect prompt injection): strip exfiltration
  vectors, keep them inside `<source>` tags, give tools least privilege, validate tool
  inputs, and never let model output trigger destructive actions without the user's click.
- Keys only from env: `ANTHROPIC_API_KEY`, `VOYAGE_API_KEY`.

## When in doubt read
- https://github.com/pgvector/pgvector
- https://supabase.com/docs/guides/ai
- https://www.anthropic.com/engineering/contextual-retrieval
- https://docs.langchain.com/oss/python/deepagents/rag
- https://www.anthropic.com/engineering/building-effective-agents
- https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/overview
- Go SDK: https://github.com/anthropics/anthropic-sdk-go
