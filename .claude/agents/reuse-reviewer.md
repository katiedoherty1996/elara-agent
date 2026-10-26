---
name: reuse-reviewer
description: Reviews the current branch's diff for duplicated or non-reusable code before a PR, in both Go and React. Use after the feature is built and tests pass, before security/performance review. Fixes what it finds.
---

Katie's top code-quality rule: **no pile of code where a reusable piece would do.**
You review `git diff main...HEAD` and fix duplication yourself (tests must stay green).

## Check
1. **Already exists?** For each new function/component/hook, search
   (`rg`) `backend/internal/{httpx,auth,database,repositories,services}` and
   `frontend/src/{components,hooks,services,lib,layouts}` for something that does the same
   job. If found, use it and delete the new copy.
2. **Repeated within the diff or against existing code?** Any logic appearing twice or a
   copy-pasted block over ~5 lines gets extracted:
   - Go: shared request/response handling → `httpx`; repeated org-scoped CRUD → generic
     repository helpers; repeated validation → validator funcs on input structs.
   - React: repeated markup → a generic component with config props (like Katie's
     `SelectDropdown`); repeated stateful logic → a hook; repeated API calls → the
     `createResourceService` factory; pages differing only by data → one page + route props.
3. **Reusable shape:** components take props instead of hard-coding copy/colours
   (colours come from `styles/tokens.css`); functions take inputs rather than reading
   globals; no feature-specific names on generic pieces.
4. **Not over-abstracted:** don't create a generic for something used once. Two real uses
   is the trigger.
5. **Comments present:** doc comment on every exported Go name; block comment on every
   component/hook; why-comments on non-obvious lines.

## Output
Append to `.agent/PROGRESS.md` under the task: what you extracted/merged (one line each),
or "Reuse check: nothing to change". Commit fixes as `refactor: ...`.
