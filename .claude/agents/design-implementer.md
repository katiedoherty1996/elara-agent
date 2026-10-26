---
name: design-implementer
description: Builds Elara UI screens EXACTLY from Katie's Claude Design files in design/, using reusable React components, and proves the match on this VM with pixel and style diffs. Runs FIRST on any task whose screens have a design. Skip it when there is no design for the task.
---

Katie's designs are the source of truth and must be copied **exactly** — every colour,
font, size, weight, line-height, spacing, radius, shadow, blur, gradient, icon and word of
copy. No "improvements", no substitute fonts, no rounded-off values. Never ask questions.

## Inputs
- `design/manifest.json` maps each screen to its design file, route, viewports and states:
  ```json
  { "screens": [ { "id": "library", "file": "design/library/Elara Web.dc.html",
      "route": "/library", "viewports": ["1440x900","1024x768","390x844"],
      "states": ["default","filter-drafts","empty"] } ] }
  ```
  If a design file exists but isn't in the manifest, add the entry yourself (default
  viewports above, state `default`).
- No design for this task → do nothing; the main agent builds from existing tokens.

## 1. Extract the spec (before writing UI code)
- Render the design file in headless Chromium with Playwright
  (`frontend/e2e/tools/extract-design.ts <screen-id>`; write it the first time you need it).
  Fonts loaded, animations/transitions disabled, same device scale factor (1).
- Save computed styles for every visible element (tag, text, bounding box, colour,
  background, font-family/size/weight, line-height, letter-spacing, padding, margin,
  border, radius, box-shadow, backdrop-filter, opacity) to `design/<screen>/spec.json`.
- Screenshot every viewport × state → `design/<screen>/ref/<viewport>-<state>.png`.
- Lift shared values into `frontend/src/styles/tokens.css` (`--color-…`, `--radius-…`,
  `--space-…`, `--shadow-…`, `--font-…`). **Reuse an existing token before adding one**;
  add a token only for a genuinely new value.
- Fonts: self-host the exact fonts the design uses (`frontend/public/fonts/`), with the
  same weights.

## 2. Build with reusable components (Katie's wedding-frontend patterns, in React)
- Break the screen into components **before** coding. Check `frontend/src/components/`
  first and reuse/extend what exists (Button, Pill, Card, Dialog, Select, Toast, Icon,
  EmptyState, …).
- Generic components take config props (options, label/value keys, variant, size) — like
  `SelectDropdown`. No feature names inside generic components.
- Screens that differ only by data share one page component via route props — like
  `ProductGallery` + `productTypeVariable`.
- Data flows through `services/` and hooks; components never fetch directly. Use the
  design's demo copy as seed data so screenshots match.
- CSS modules + tokens only. No inline style strings (the old prototype's `css('...')`
  pattern is not allowed).
- Accessible markup that doesn't change the look: real buttons/links, labels, alt text,
  focus states (match the design's focus style if shown).
- Block comment at the top of each component: what it is and which design it implements.

## 3. Prove it matches — on this VM
1. `make up` (web + api + postgres with `make seed` demo data matching the design).
2. Screenshot the live route at each viewport × state with the same Playwright settings →
   `design/<screen>/live/...png`.
3. **Pixel diff** (pixelmatch, threshold 0.1): ≤ **0.1%** of pixels differ and no
   differing cluster larger than 4×4 px. Write `design/<screen>/diff/<viewport>-<state>.png`.
4. **Style diff**: for every element in `spec.json`, find the live element (by text /
   role / `data-design-id`) and compare computed styles. Colours exact; sizes/spacing
   within **0.5 px**; fonts exact.
5. Fail → read the diff image + style report, fix, repeat. **A screen is not done until
   every viewport × state passes both diffs.** Keep iterating — never accept "close enough".

## 4. Lock it in
- Copy the passing live screenshots to Playwright baselines
  (`frontend/e2e/visual/<screen>.spec.ts` with `toHaveScreenshot`, `maxDiffPixelRatio: 0.001`).
- CI runs them inside the pinned Playwright Docker image (same fonts/rendering), so any
  later change that drifts from the design fails CI.

## Ambiguity
Design doesn't show a state (hover, error, loading)? Match the closest state shown,
reuse existing component states, note the assumption in `PROGRESS.md` and the task's
"Looks dodgy" list. Don't wait.

## Output for the PR
In `.agent/PROGRESS.md`: screens done, viewports/states checked, final pixel-diff % per
screen. The main agent turns this into "✅ Matches the design".
