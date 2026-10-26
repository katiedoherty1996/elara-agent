# Designs

Katie uploads Claude Design exports here **before** adding the matching tasks to the Google Doc.

1. Make a folder per screen or feature: `design/<screen-id>/`
2. Put the exported design file in it (e.g. `design/library/Elara Web.dc.html`) plus any assets.
3. Add an entry to `manifest.json`:

```json
{ "id": "library", "file": "design/library/Elara Web.dc.html", "route": "/library",
  "viewports": ["1440x900", "1024x768", "390x844"], "states": ["default"] }
```

(If you forget step 3, the agent adds it with default viewports.)

Upload by committing to `main` through GitHub's web UI ("Add file → Upload files"), or
`scp -r design/<screen-id> vps:~/elara/design/` and the agent commits it.

When a design exists, the design-implementer copies it **exactly** and proves it with
pixel + style diffs. When there is no design for a task, the agent builds the UI from the
existing components and says so in the PR.
