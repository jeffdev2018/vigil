# Repository Instructions

Multica is a task management platform where people and agents collaborate on issues. These instructions apply to all coding agents working in this repository.

## Scope and Reading Order

- Before changing `apps/mobile/`, also read [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md), even if your tool does not load nested instructions automatically. Platform-specific sections below apply only to the named platform.
- For naming, translations, or Chinese UI/docs copy, read [conventions.mdx](apps/docs/content/docs/developers/conventions.mdx) and [conventions.zh.mdx](apps/docs/content/docs/developers/conventions.zh.mdx).
- Maintain shared rules here and mobile-specific rules in the mobile file. `CLAUDE.md` files only import them. Update instructions in the same change that alters the referenced workflow or boundary; do not add dependency version lists or duplicate rules.
- Rules here are meant to be hard to infer from code or easy to get wrong. Keep them short and authoritative.

## Sharing Rules and Package Boundaries

| Location | Responsibility and constraints |
| --- | --- |
| `server/` | Go backend; Chi, sqlc, WebSocket |
| `packages/core/` | Headless logic, API client, Query hooks, shared Zustand stores. No UI libraries, `react-dom`, `localStorage`, or `process.env`; use `StorageAdapter` for persistence. |
| `packages/ui/` | UI primitives and shared styles. No business logic or `@multica/core` imports. |
| `packages/views/` | Shared web/desktop pages and business components, organized as `packages/views/<domain>/`. No store definitions, `next/*`, or `react-router-dom`; use `NavigationAdapter`, `useNavigation()`, and `<AppLink>`. |
| `apps/web/` | Next.js routes/layouts and web-only UI. `apps/web/platform/` is the only place for Next.js navigation/platform APIs. |
| `apps/desktop/` | Electron and desktop-only UI/state. `apps/desktop/src/renderer/src/platform/` is the only place for `react-router-dom` navigation wiring. |
| `apps/mobile/` | Independent Expo/React Native client: owns UI, state, hooks, providers, i18n, React version, build, and release. Shares core types and pure utilities, including platform-independent schemas. |
| `apps/docs/` | Fumadocs documentation site |
| `packages/tsconfig/`, `packages/eslint-config/` | Shared TypeScript and ESLint config |

- Dependency direction is `views -> core + ui`; core and ui remain independent. Shared packages export raw TypeScript compiled by consuming apps.
- Extract logic used by both web and desktop into the appropriate shared package: headless logic to `packages/core/`, shared business views to `packages/views/`, shared primitives to `packages/ui/`. Keep framework/Electron/router APIs in the app/platform layer; inject platform-specific UI through props/slots.
- Wire shared features into both `apps/web/app/` and the desktop router or overlay, and use `useNavigation().push()` or `<AppLink>` in shared code. Reuse existing guards/providers such as `DashboardGuard` in `packages/views/layout/`.
- Each workspace under `apps/` and `packages/` declares its directly imported external dependencies. Use `catalog:` from `pnpm-workspace.yaml` for shared dependencies; mobile pins Expo/React Native dependencies in its own manifest.

## Development and Verification

Use `Makefile`, workspace `package.json` files, and `pnpm-workspace.yaml` for current commands and versions. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and worktree operations.

- Use the checkout's managed environment: `make up` (`C=api,web,daemon,desktop`), `make status`, `make down`. `make down` preserves data; `make destroy` removes the environment and its data (database, profile, daemon workspaces, Desktop userData, registry entry).
- `make up` records each environment in `~/.multica/dev/` and allocates API/Web/Desktop ports and the database name under a lock instead of recomputing them from the path. It verifies the database through `DATABASE_URL`, never `docker exec` — a `docker exec` create lands in the wrong server whenever a native PostgreSQL owns 5432. It reuses an API only when `/health` proves the listener pid, process group, and commit belong to this checkout.
- `make list` shows every environment on this machine; `make gc` collects agent-owned TTL environments (also attempted best-effort on the next `make up`); `make db-drop` drops this checkout's database; `make remove-worktree WORKTREE=../path` drops a linked worktree's database and removes it.
- Worktrees share PostgreSQL but have isolated databases/ports. Use the environment scripts and `.env.worktree` (`make worktree-env`, `make setup-worktree`, `make start-worktree`; `make dev` auto-detects); do not copy the main checkout's `.env` or manually create a database through an assumed PostgreSQL instance. Direct `pnpm dev:desktop` self-isolates from the path; `make up C=desktop` overrides that with the registry-allocated renderer port and app name.
- Regenerate sqlc with `make sqlc` after SQL changes.
- Run the narrowest useful checks while iterating, then broaden when risk warrants it. Report what actually ran and any skipped checks; never claim a check passed unless you ran it.

Run these from the repository root:

| Scope | Checks |
| --- | --- |
| Frontend excluding mobile | `pnpm typecheck`, `pnpm lint`, `pnpm test` |
| Go backend | `make test` |
| End-to-end | `pnpm exec playwright test` |
| Combined web/backend verification | `make check` |
| Mobile | Commands in [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md#verification) |

Root frontend commands and `make check` do not verify mobile. Docs-only changes can use link/reference checks and `git diff --check`; state that code tests were not run.

## State Rules

- TanStack Query owns API/server data. Zustand owns client state such as filters, drafts, modals, tab layout, and navigation history; persist only durable preferences/drafts/layout, not server data or ephemeral UI state.
- Web/desktop shared stores live in `packages/core/`. Desktop platform stores remain in desktop; mobile stores remain in mobile. Do not define stores in `packages/views/`.
- On web/desktop, workspace identity is route-driven; platform mirrors exist only for request headers, storage namespaces, and reconnects. React Context is for platform plumbing such as `WorkspaceIdProvider` and `NavigationProvider`, not a second server-state store.
- Among stores, only auth/workspace stores may call `api.*` directly; other server interactions belong in queries/mutations.
- Workspace-scoped query keys include `wsId`; account-level keys remain account-scoped. Hooks needing workspace context accept `wsId` unless guaranteed to run under its provider.
- Zustand selectors return stable references; use shallow comparison for allocated objects/arrays.
- WebSocket events patch or invalidate Query caches, not server payloads in Zustand. Clearing client-owned pointers (active session, selection, current workspace) is allowed with one responder and a self-initiated guard when this client can cause the event.
- Optimistic field patches require ALL of: a predictable result, rare failure, trivial rollback, and staying on the current screen. Canonical cases are status/assignee/toggle patches. Snapshot before patching, roll back on failure, and invalidate uncertain projections on settle.
- Create/delete/leave and confirmation flows await the server before navigation or cleanup; do not optimistically delete entities. Exception: mobile inbox mark-read, as documented in its instructions.
- Message sends use visible pending state and retry on failure, not silent optimism.

## API Compatibility

Installed desktop clients may talk to newer backends. Preserve response compatibility at the API boundary.

- UI-consumed JSON passes through a zod schema and `parseWithFallback`, not an `as T` cast. Web/desktop use `packages/core/api/schema.ts`; mobile uses its own request helpers.
- Provide defaults for optional fields, optional-chain downstream, and add fallbacks for unknown server enums: every server-driven enum `switch` needs a `default` branch.
- Prefer explicit boolean checks (`=== true`); avoid tying critical affordances to a single backend flag when other contract signals are available.
- When adding/changing an endpoint, update its schema and malformed-response tests.

## Database and Migration Rules

Hard requirements for every new or modified database design and production migration:

- Do not add foreign keys (`FOREIGN KEY` / `REFERENCES`), cascading deletes, or cascading updates. Validate relationships and clean up dependents in application code, using a transaction when the operation must be atomic.
- Every migration-created index, including indexes on new tables, uses `CREATE [UNIQUE] INDEX CONCURRENTLY`. PostgreSQL rejects concurrent index creation inside a transaction or a multi-command string, so each concurrent index build gets its own single-statement migration file; the runner executes files outside an explicit transaction.
- Conditionally skipped migrations are still recorded in `schema_migrations`, so the ledger proves ordering, not that every migration's SQL executed. Later DDL touching conditional objects must be idempotent (`IF EXISTS` / `IF NOT EXISTS`); document recovery if the missing object would break runtime behavior.

## Backend UUID Rules

In `server/internal/handler/`, distinguish UUID sources before using them in writes:

- UUID-or-human-readable resource params: resolve with loaders such as `loadIssueForUser`, `loadSkillForUser`, `loadAgentForUser`, or `requireDaemonRuntimeAccess`, then write using the resolved `entity.ID`.
- Pure UUID request input: `parseUUIDOrBadRequest(w, s, fieldName)`; return immediately when `ok=false`.
- Trusted sqlc/test-fixture round-trips: `parseUUID(s)`, which panics on invalid input.
- Outside handlers: `util.ParseUUID(s) (pgtype.UUID, error)` and check the error.

Workspace-scoped queries filter by `workspace_id`; membership gates access and `X-Workspace-ID` selects the workspace. Assignees are polymorphic: interpret `assignee_id` together with `assignee_type` (member or agent).

## Desktop Rules

- Workspace session routes are tab destinations such as `/:slug/issues`. Pre-workspace one-shot flows (create workspace, accept invite) register a `WindowOverlay` type in `apps/desktop/src/renderer/src/stores/window-overlay-store.ts`; do not add them to `routes.tsx`. Stale workspace tabs heal by dropping stale tab groups, not by rendering desktop error pages.
- Workspace route layouts own `setCurrentWorkspace(slug, uuid)` from `@multica/core/platform`; leaving workspace context calls `setCurrentWorkspace(null, null)` explicitly.
- Cross-workspace navigation uses the adapter's `switchWorkspace(slug, targetPath)` flow; do not bypass it with direct router navigation.
- Workspace delete and leave both await the server, then navigate. Each is paired with a self-initiated registry in `packages/core/workspace/pending-delete.ts` (`workspace:deleted` for delete, `member:removed` for leave) so the realtime handler does not answer this client's own request with a parallel relocate. A new flow whose own action echoes back over the socket needs the same guard.
- Full-window views outside the dashboard shell mount `<DragStrip />` from `@multica/views/platform` as the first flex child. Interactive controls in the top 48px need `WebkitAppRegion: "no-drag"`.

## UI Copy

- Descriptions are optional and omitted by default. Do not restate titles, labels, values, statuses, or button actions. Add help only for a non-obvious choice, constraint, consequence, or next step; state each fact once beside the relevant control.
- Keep permissions, cost, destructive consequences, execution prerequisites, and error recovery visible when relevant. Put advanced usage and diagnostics in accessible, explicit help. Preserve labels and accessible names; do not move redundant prose wholesale into `sr-only` text.
- Review copy with its surrounding controls and all supported translations, including mobile's independent copy. Follow the UI copy rules in the existing conventions pages; a description prop is not a requirement to write a paragraph.

## Web/Desktop UI Rules

- For Button and Dialog usage, read `packages/ui/docs/button.md` and `packages/ui/docs/dialog.md`. These component contracts also power UI Lab documentation.
- Prefer existing shadcn/Base UI primitives over custom implementations. Add components with `pnpm ui:add <component>` from the repo root.
- The Pro `@reui` registry is configured in `packages/ui/components.json`. For `pnpm ui:add @reui/<name>`, answer `n` to every overwrite prompt so local customizations survive. `REUI_LICENSE_KEY` comes from the environment (agents get it from their Multica agent environment); never write it into a repo file.
- ReUI ships source, not a dependency: adapt vendored primitives into `packages/ui/components/ui/` and compositions into `packages/views/<domain>/`, rewritten to our conventions before committing.
- Use shared semantic tokens from `packages/ui/styles/` (`bg-background`, `text-muted-foreground`); avoid hardcoded colors and duplicated base styles. Typography uses the role-named `--text-*` scale in `packages/ui/styles/tokens.css` (`text-caption`, `text-body`, `text-title`, …), which is the authoritative list, not Tailwind's default `text-sm` / `text-base` ramp.
- Selected states must remain identifiable while hovered. Express the active state on a dimension hover does not touch (weight, text color), or define the `data-active:hover:` compound explicitly — otherwise hovering a selected row downgrades it to plain hover.
- Handle overflow, long text, scrolling, alignment, and spacing deliberately; avoid unnecessary local state, and prefer more spacing over adding a divider.

## Testing

- Tests live beside their implementation: shared logic and stores/queries/hooks in `packages/core/*.test.ts`, shared components/pages/forms/modals in `packages/views/*.test.tsx`, platform wiring (cookies, redirects, search params) in `apps/web/*.test.tsx` or `apps/desktop/`, E2E in `e2e/*.spec.ts`, Go tests in `server/`. Do not test shared behavior in app suites.
- Give each behavior one canonical test layer: helper tests own parsing/state matrices; component tests cover wiring, accessibility, happy paths, and named regressions, and point at the canonical file in a comment. Do not re-run a helper's matrix through a DOM mount. Prefer a failing regression test in the correct package before behavioral fixes.
- DOM-free `.test.ts` files start with `// @vitest-environment node`; do not use it if it would silently switch the code under test to an SSR path (`typeof window` / `document` branches).
- Views tests must not mock `next/*` or `react-router-dom`. Mock stores with their Zustand callable shape (`selectorFn` plus `getState`); mock API calls at `@multica/core/api`.
- E2E setup/teardown uses `TestApiClient`.
- DB-backed Go tests use `server/internal/testutil` fixtures (`dbfx.Issue`, `dbfx.Task`, `dbfx.Insert`) and `testutil.Call(h, req).Want(status).JSON(&out)`. Do not open-code `INSERT ... RETURNING id` with a matching `t.Cleanup(DELETE ...)`, or a `httptest.NewRecorder()` / status-check / decode quartet. Keep product assertions and case-specific diagnostics in the test, not in fixture helpers: `.Want()` prints the request line, both statuses, and the body, but does not know which case in a loop was running.
- Default tests must not resolve or execute user-installed agent CLIs; pass test-created fake or missing executable paths. New default agent commands go in `scripts/agent-cli-command-names.txt`.
- Only run real-agent smoke tests when explicitly authorized. Gate them behind `agentintegration` and check `MULTICA_RUN_REAL_AGENT_SMOKE=1` before executable lookup/account access. Run the specific test: `(cd server && MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent -run '<test-name>' -count=1 -v)`. It may access an authenticated account and consume quota.

## Change and Delivery Rules

- Keep changes scoped; reuse existing patterns and components instead of new parallel abstractions. Avoid broad refactors unless the task requires them. Code comments are English.
- TypeScript strict mode is enabled; keep types explicit. Go follows standard conventions: `gofmt`, `go vet`, checked errors.
- Do not add internal compatibility shims, dual writes, fallback paths, or legacy adapters unless requested. This does not relax API response compatibility above.
- When a flow or API is being replaced and the product is not live, remove the old path instead of preserving both.
- New global pre-workspace routes use a single word (`/login`, `/inbox`) or `/{noun}/{verb}` (`/workspaces/new`), not hyphenated root names like `/new-workspace`. Update `server/internal/handler/reserved_slugs.json`, run `pnpm generate:reserved-slugs`, and commit `packages/core/paths/reserved-slugs.ts` when changing reserved slugs.
- When changing CLI commands/flags, API fields, or product behavior documented by built-in skills under `server/internal/service/builtin_skills/*`, update the relevant `references/<domain>.md` in the same change.
- Use atomic conventional commits (`feat(scope)`, `fix(scope)`, `refactor(scope)`, `docs`, `test(scope)`, `chore(scope)`) and the repository PR template. A production deployment requires a `v0.x.x` release tag on `main`; follow [.github/RELEASING.md](.github/RELEASING.md) and default to a patch bump unless specified otherwise.
