# Workspace Brain source map

- `server/cmd/multica/cmd_brain.go` registers `list`, `show`, `save`, and `archive`. `save`
  creates by default and updates when `--id` is given; the update path first GETs the note
  and sends its `revision` back on the PATCH, which is what turns a concurrent edit into a
  409 instead of a silent overwrite. `--content` and `--content-file` are mutually
  exclusive; `--content-file -` reads stdin.
- The CLI maps to `/api/workspace/notes` (list, create), `/api/workspace/notes/{id}` (get,
  patch, delete) and `/api/workspace/notes/{id}/archive` + `/unarchive`
  (`server/internal/handler/workspace_note.go`, routed in `server/cmd/server/router.go`).
- Authorization: every workspace member reads and writes. Delete is narrower —
  `canDeleteWorkspaceNote` allows a workspace owner/admin or the note's own author. A run
  authenticated with a task token resolves through `Handler.resolveActor` to its agent, so
  notes saved from a run are stored with `source='agent'`, `source_agent_id` and
  `source_task_id`.
- Validation lives in `validateWorkspaceNoteTitle` / `validateWorkspaceNoteContent` /
  `normalizeWorkspaceNoteTags`: title 1–200 runes, content ≤ 20000 runes, at most 10 tags of
  ≤ 50 runes each, lowercased, de-duplicated and sorted. The table's CHECK constraints
  (migration 625) are the backstop.
- Search is Postgres full-text: `to_tsvector('simple', title || ' ' || content)` with a GIN
  index (migration 629), queried with `plainto_tsquery`. `'simple'` — no stemming, no
  stopwords — because the corpus is polyglot engineering prose.
- Run injection: `TaskService.LoadWorkspaceNotesForBrief` (`server/internal/service/task.go`)
  returns every pinned note plus the 20 most recently updated others;
  `server/internal/handler/daemon.go` attaches them to the claim response as
  `workspace_notes`; `writeWorkspaceKnowledge`
  (`server/internal/daemon/execenv/workspace_knowledge.go`) writes
  `.multica/knowledge/README.md` plus one `<slug>-<id8>.md` per note under a 200 KB budget;
  `writeWorkspaceKnowledgeSection` (`runtime_config_sections.go`) emits the brief's
  "Workspace Knowledge" section. The load is non-blocking: a failure costs the run its
  knowledge section, never its dispatch.
- Curation: `server/internal/service/workspace_note_curation.go`, scheduled daily by
  `scheduler.WorkspaceBrainCurationJob` (`server/internal/scheduler/jobs_brain_curation.go`).
  A workspace is curated only when at least 2 live notes changed since the previous pass.
  The LLM returns a plan `{merge, retitle, tag, archive}`; merges archive the source note
  with `merged_into` set. Without a configured LLM the pass logs once and no-ops.
- UI: `packages/views/brain/components/brain-page.tsx`, route `/{slug}/brain` in
  `apps/web/app/[workspaceSlug]/(dashboard)/brain/page.tsx` and
  `apps/desktop/src/renderer/src/routes.tsx`.
