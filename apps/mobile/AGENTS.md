# Mobile App Instructions

These rules apply only to `apps/mobile/`, in addition to the [root instructions](../../AGENTS.md). Paths below are relative to `apps/mobile/` unless prefixed with `packages/` or `.github/`.

## Pre-flight — Before Writing Code

For any new mobile feature, screen, or interaction, complete these steps in order. Skipping a step means no code yet; read-only investigation and answering questions are exempt. This gate is specific to this repository and overrides the root rule that an explicit implementation request alone authorizes work.

1. **Read the real web/desktop implementation.** Name the relevant code before reasoning from general experience: `packages/views/<feature>/` for UI shape and density, `packages/core/<feature>/{queries,mutations,ws-updaters}.ts` for endpoints, cache key shapes, optimistic patches, and WS coverage, plus anything matching `*-display.ts`, `dedupe*`, `coalesce*`, or `useMemo(() => transform(raw))`. List the must-agree points: counts, enums, permissions, cross-cache side effects, navigation flow.
2. **Show the interaction plan and parity points** (readable in 30 seconds): what you will build in one sentence, the container/interaction chosen by the waterfall in [UI Components](#ui-components), the parity points from step 1, what UI must differ and why, and the visual baseline check (screen titles, tab-bar icons, vertically stacked trailing row elements, type-aware secondary labels; compare a web screenshot with a simulator screenshot).
3. **Wait for an explicit imperative** ("build X", "change X", "go", "start"). "Yes", "right", "sounds good", or a question like "how should we do X?" is not authorization.

Ask when an unresolved product decision affects the implementation.

## Sharing and Dependencies

- Mobile owns its UI, state, hooks, providers, API client, QueryClient, i18n, and build/release workflow. Import core types with `import type`; runtime imports are limited to pure utilities and platform-independent schemas (for example the pure zod exports in `packages/core/api/schemas.ts`). Do not import web/desktop stores, hooks, Query factories, or WS updaters.
- Use `package.json` and the lockfile for current versions. Mobile pins Expo/React Native dependencies rather than taking the root React catalog.
- Add SDK-aligned native packages with `pnpm exec expo install <package>` from this directory. Check `pnpm view <pkg> dist-tags` before adding anything else; never pick versions from memory. `pnpm add <pkg>` installs npm `latest`, which often outpaces the Expo SDK and breaks at runtime.
- Follow [README.md](README.md) for setup, simulator/device builds, and environment variants. Keep iOS scripts routed through `scripts/ios-run.sh`, which prebuilds before running iOS so config plugins are reapplied: `expo run:ios` prebuilds only when `ios/` is missing, so without the explicit prebuild everything `app.config.ts` owns (icon, bundle id, display name, URL scheme, Info.plist permission strings) stays pinned to the first prebuild while the build still reports success. Preserve the caller's `APP_ENV`; avoid clean prebuilds in the normal edit loop (`expo-build-properties` sets `buildReactNativeFromSource`), and run `npx expo prebuild -p ios --clean` by hand when a native dependency changes.
- Generated `ios/` and `android/` directories are not source. Check new source paths with `git check-ignore -v <path>` when they could match the root ignore rules, particularly `data/`, `build/`, and `bin/`; add `!<dir>/` and `!<dir>/**` to `apps/mobile/.gitignore` when a rule matches, then confirm with `git ls-files <dir>`.

### Locked stack baseline

This list exists so agents do not propose outdated alternatives. Read exact versions from `package.json`; update this list only when a choice actually changes.

- Expo SDK 55 with Expo Router (file-based routing, version aligned with the SDK), React Native, and React pinned in `apps/mobile/package.json`, not via the root `catalog:`.
- TypeScript strict.
- NativeWind 4 + Tailwind 3.4. NativeWind 5 is unstable; stay on v4. Web/desktop use Tailwind v4 — the versions differ intentionally and mobile does not import web CSS.
- react-native-reusables (RNR): the shadcn equivalent for React Native (NativeWind + RN-Primitives + CVA).
- TanStack Query 5, with mobile's own `QueryClient` (`AppState` focus listener, `NetInfo` online listener).
- Zustand for mobile-local state only.
- `expo-secure-store` for auth token persistence and the theme preference.
- `expo-audio` for chat conversation recording and metering. Its microphone permission string comes from the `expo-audio` config plugin in `app.config.ts`; the general camera/media plugin keeps `microphonePermission: false`.

## Behavioral Parity

- Before implementing a feature, inspect its web/desktop implementation in `packages/views/` and `packages/core/`: endpoints, permissions, state transitions, display transforms, and cross-cache side effects.
- Match product semantics: counts/visibility under identical pagination and coalescing rules, permissions derived from `packages/core` rather than from feel, every state enum/transition rendered with a fallback for unknown values, and canonical IDs/fields. Never silently drop a category.
- Adapt UI and navigation to the phone; document meaningful divergences at the divergence point, naming the web-side source function they preserve.
- Mirror the relevant behavior using mobile-owned caches and helpers; do not copy cache shapes or UI preprocessing blindly. List transforms must preserve pagination, grouping, and visibility semantics.
- Before rendering any API list response, grep `packages/core/<domain>/queries.ts` and `packages/views/<domain>/components/*.tsx` for preprocessing (`dedupe*`, `coalesce*`, `filter*`, `*-display.ts`, `useMemo(() => transform(raw))`) and mirror everything that runs between `useQuery` and the JSX. The backend returns the raw cache shape, not what should be displayed.
- Reuse `lib/inbox-display.ts` for inbox list display. Badge counts come from the server unread summary via `lib/unread-counts.ts` and `data/queries/inbox.ts`; do not derive them from the downloaded list. Inbox writes/events refresh both the list and unread summary through `data/realtime/inbox-ws-updaters.ts`.
- Explain significant interaction choices and parity points briefly; do not require a second approval using special wording beyond the pre-flight gate above.

### Documented incidents

Kept in this repository as reflexes, not history. Each one cost real debugging time.

- **Inbox counts disagreed (2026-05-09).** Web showed "Inbox 1" while mobile rendered 3+ unread dots for the same user and moment. `GET /api/inbox` returns raw rows including archived items and multiple notifications per issue; web/desktop run them through `deduplicateInboxItems` (`packages/core/inbox/queries.ts`: drop archived, group by `issue_id` keeping the newest, sort by `created_at` desc) before rendering and counting. Mobile rendered the raw list. Fixed by mirroring the transform into `lib/inbox-display.ts` and running the inbox tab through it before rendering and before any counting. The same hazard exists for timeline coalescing and comment-thread flattening.
- **Stuck pull-to-refresh spinner (2026-05-11).** `data/api.ts` had no timeout, no `AbortController`, and no caller-`signal` plumbing. iOS suspends backgrounded apps and can kill in-flight network tasks, leaving a Promise that never settles; TanStack Query then waits on the dead Promise instead of refetching, so `isRefetching` stayed `true` forever. Fixed by the three-part rule in [Data Layer](#data-layer): hard timeout, `opts.signal` on read methods, `signal` forwarded by every `queryFn`.
- **Committed but untracked source.** `apps/mobile/data/` once had 14 source files missing from the git tree while `git status` was clean, because the repo-root `.gitignore` swallowed `data/`. Metro reads the filesystem, so local builds worked and CI/clones would have failed. Hence the `git check-ignore` step above.

## UI Components

- Inspect existing rows, pickers, forms, and domain visuals before adding components: `components/inbox/`, `components/issue/`, `components/project/` for list rows; `components/issue/pickers/` and `components/project/pickers/` for pickers; `components/ui/status-icon.tsx`, `priority-icon.tsx`, `actor-avatar.tsx` for domain visuals; `app/(app)/[workspace]/issue/[id]/`, `chat/`, `new-issue.tsx` for screen structure. Extend a suitable existing pattern; do not rewrite a domain component merely because a new feature uses it, and do not fork a second variant of a near-fit.
- For a new interaction, prefer a native iOS/RN API, then an RNR component. If neither fits, compose existing primitives inline for a local need. A new generic primitive requires at least three callers and no suitable native/RNR alternative; clarify unresolved interaction requirements before inventing one.
- Native examples: `Alert.prompt` for text prompts, `Alert.alert` for confirmation, `ActionSheetIOS.showActionSheetWithOptions` for action menus, `@react-native-community/datetimepicker`, `expo-image-picker`, `expo-document-picker`, `Share.share`, and `expo-haptics`.
- Add RNR components with `npx @react-native-reusables/cli@latest add <name>`. Review generated changes and preserve local customizations. Keep default variants/sizes/spacing unless a concrete product need requires changes; do not add wrapper layers or "improved" defaults.
- Generic primitives live in `components/ui/`; domain compositions live in `components/<domain>/`. Use the existing `cn()` in `lib/utils.ts` and semantic tokens. Some `components/ui/` files predate RNR adoption — do not use one as a template when an RNR equivalent exists.
- Upgrade an older domain component only when you are already modifying that file for a different reason. Note other smells in the PR description as follow-ups instead of widening scope.
- Screens have titles, tab bars have icons, secondary labels use type-aware helpers, and multiple trailing row elements stack vertically. Check long text, keyboard, safe areas, scrolling, and both themes.

### Theming model

- `global.css` defines mobile color variables under `:root` (light) and `.dark:root` (dark); `tailwind.config.js` maps classes like `bg-background` to `hsl(var(--background))` and `lib/theme.ts` supplies native style/navigation values. Update the corresponding mappings together when changing tokens.
- `darkMode: 'class'`, not media query, so the in-app Appearance picker (`light` / `dark` / `system`) can override the OS preference. Switch modes through `lib/use-color-scheme.ts` (`setColorScheme`); classes rebind reactively, with no manual className toggling.
- The preference persists in `expo-secure-store` and is loaded at startup in `app/_layout.tsx` before the first paint; a missing value defaults to `system`. React Navigation is themed separately with `NAV_THEME` from `lib/theme.ts`.
- Keep mobile tokens local; do not import web/desktop CSS. Read [docs/markdown-rendering-adr.md](docs/markdown-rendering-adr.md) before changing the Markdown renderer or its native styling.

### Sheets and navigation

| Content | Container |
| --- | --- |
| Confirmation or text prompt | Native alert/prompt |
| Short action menu | Native action sheet; reuse existing local popover patterns when appropriate |
| Long list, search, form, or keyboard interaction | Expo Router `presentation: "formSheet"` route |
| Multi-screen modal flow | Expo Router `presentation: "modal"` |

- All pickers in one attribute row use the same formSheet interaction, including short option lists: mixing centered cards with formSheets in the same chip row gives the user two different gestures depending on which chip they tap. Centered cards stay correct for isolated short menus with no neighbour.
- Place picker routes under their owning context so the URL reads sensibly, and register them in `app/(app)/[workspace]/_layout.tsx` using `SHEET_OPTIONS`. Reuse that configuration rather than copying its current values; document necessary overrides at the route. Each value there exists for a known platform behavior — explicit numeric detents because `"fitToContents"` is broken on iOS 26 + Expo 55 (expo/expo#42904, #42965), a visible grabber because users do not discover the gesture, a corner radius matching RNR cards, and an explicit body height as a safety net against zero-size sheets. Isolated sheets with no chip-row neighbour may override the detents only.
- Sheet bodies own their data lookup and mutation, then return with `router.back()`; no callbacks up to a parent. Draft flows use the appropriate local draft store when there is no persisted record, because a route cannot share state with the modal that opened it. Follow the existing body-header pattern (`headerShown: false`, header drawn inside the body) unless the route explicitly requires native header chrome.
- Check the return destination when opening a sheet from another modal (a formSheet pushed from a `presentation: "modal"` route returns to that modal, not the tab), and handle deep links without assuming the record is already cached. Android falls back to a regular modal; note it inline when a feature must behave identically on both.
- Destructive swipes reveal an action that requires a tap; never auto-execute on full swipe. Reuse `components/inbox/swipeable-inbox-row.tsx`, including its one-shot haptic feedback when the drag crosses the action width.

## Data Layer

Use the existing helpers rather than rebuilding request or subscription plumbing. New code that reinvents them is a review block: they encode signal forwarding, schema fallback, ordering, and payload typing.

| Concern | Implementation |
| --- | --- |
| Requests | `data/api.ts`: `fetchValidated` for GET, `fetchValidatedWith` for writes with consumed response bodies |
| Response schemas/fallbacks | `data/schemas.ts`, `lib/parse-response.ts`; reuse compatible pure schemas from core |
| Query keys and options | `data/queries/`; mutations import the corresponding key factory |
| Query lifecycle | `data/query-client.ts`: AppState focus and NetInfo connectivity |
| Realtime lifecycle | `data/realtime/ws-client.ts`, `data/realtime/realtime-provider.tsx`, `lib/use-ws-subscriptions.ts` |

- UI-consumed responses follow the root API compatibility rules. Raw `this.fetch<T>()` is reserved for writes whose response body is unused — that is the only place an `as T` is acceptable. Fallback values must satisfy the expected success type exactly (see the `EMPTY_*` pattern in `data/schemas.ts`). The `endpoint` telemetry label defaults to the path; override it only to keep dynamic segments grouped (`GET /api/issues/:id`).
- Three-part cancellation rule; partial adoption leaves a footgun. (1) `data/api.ts` keeps a hard request timeout (`FETCH_TIMEOUT_MS`) built from a manual `AbortController` plus `setTimeout` — Hermes implements neither `AbortSignal.timeout()` (facebook/react-native#42042) nor `AbortSignal.any()`, so combine signals by attaching an `"abort"` listener manually. (2) Every read-side method accepts `opts?: { signal?: AbortSignal }` and passes it to `fetch`. (3) Every `queryFn` destructures and forwards `{ signal }`; `grep -n "queryFn: () =>" data/queries/` must stay empty. The AppState/NetInfo wiring in `data/query-client.ts` cannot replace this — a refetch attempt does nothing while a dead Promise is still pending.
- Preserve the API client's `X-Request-ID` per request and its two-line structured request log (`[api] → METHOD path`, `[api] ← STATUS path`, with request id and duration), and the idempotent 401 cleanup in `app/_layout.tsx` (auth, workspace, Query cache, navigation), guarded so simultaneous 401s sign out once. Mobile uses bearer auth; do not copy browser cookie/CSRF plumbing.
- Use feature key factories in `data/queries/<feature>.ts`, with `wsId` for workspace data and separate account-level keys for cross-workspace summaries. Keep the web-matching shape (`all(wsId)` then narrower keys) so prefix invalidation works, do not hardcode key strings anywhere else, and do not require every key to have the same number of segments.
- **Optimistic-update exception:** inbox mark-read may patch synchronously before navigation to avoid an unread row in the iOS transition snapshot. `setQueryData` must run in `onMutate` *before* `await qc.cancelQueries(...)`, because the await yields a microtask during which iOS captures the source-view snapshot. Keep this inside the mutation (see `useMarkInboxRead.onMutate` in `data/mutations/inbox.ts`), capture rollback state before patching, and refresh on settle. It is not permission to optimistically create/delete/leave or to generalize navigation-time optimism.
- Other mutations follow the root optimistic-update policy; message sends retain visible pending/retry states.

## Realtime

Mobile uses the same WS protocol as web/desktop but mounts subscriptions differently: cellular data cost, AppState lifecycle, per-screen cleanup, and a smaller cache surface make a direct port of web's pattern wrong.

- Three layers: `data/realtime/ws-client.ts` (single socket, no React, backoff with jitter, idle/active/paused lifecycle), `data/realtime/realtime-provider.tsx` (owns the client, remounts on auth/workspace/AppState/NetInfo changes), and `use-<feature>-realtime.ts` hooks (events to cache mutations). Only the third layer changes when adding event coverage.
- Mobile has no centralized `useRealtimeSync` equivalent. Keep listing subscriptions mounted for the workspace session in `RealtimeSubscriptions` in `app/(app)/[workspace]/_layout.tsx` (keyed on `wsId` only). Record subscriptions belong to the owning screen, take the route id, filter every event by that id, and clean up on unmount/ID changes. Do not mount a per-record hook globally "to be safe".
- Use `useWSSubscriptions` and typed `ws.on<E>()`; the handler payload is already typed, so do not add `as XxxPayload` casts that hide drift. When adding protocol events, update the `WSEventType` union, the payload interface, and the `WSEventPayloadMap` entry in `packages/core/types/events.ts`; a missing map entry yields `unknown`, which is the safety net. Narrow unknown payloads rather than adding unchecked casts.
- Updaters belong to the feature whose cache they change, and that feature subscribes to the relevant foreign events: `data/realtime/inbox-ws-updaters.ts` owns `patchInboxIssueStatus` / `dropInboxItemsByIssue`, and `use-inbox-realtime.ts` subscribes to `issue:updated` and `issue:deleted`. If you reach across features from an issue hook to patch inbox, the ownership is inverted.
- Mirror web's updaters, never import them. Web's updaters bind to `packages/core` key factories (a different runtime instance from mobile's) and carry branches for caches mobile does not have (status-bucketed lists, children subtree, label-by-issue). Document the mirror at the top of the mobile file.
- Patch when the payload and cache shape make the result determinate; a `setQueryData` is free while an `invalidateQueries` costs a roundtrip per key on cellular. Invalidate only when the payload is just an id, list membership is uncertain, the event is rare enough that refetching is simpler, or after a reconnect. Use existing refresh helpers that handle in-flight request races.
- Each feature refreshes only the caches it owns on reconnect, including any account-level projections. Avoid a global refetch sweep, and do not subscribe to an event with no mobile consumer.
- Accept authoritative WS updates over local optimistic state; brief flicker is acceptable and correctness wins. Do not add timestamp gates to protect optimistic patches without a demonstrated ordering problem.

## Verification

From the repository root:

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
```

- Root frontend checks exclude mobile (`--filter='!@multica/mobile'` in `.github/workflows/ci.yml`), so mobile failures do not block web/desktop PRs. `.github/workflows/mobile-verify.yml` defines the current mobile CI scope (path-filtered on `apps/mobile/**` and `packages/core/**`); these checks do not build an IPA or verify native rendering.
- Mobile releases are decoupled from the root `v*.*.*` tags. There is no mobile release workflow in the repository today; confirm the current process before claiming a release path.
- For UI changes, verify the affected flow in the simulator/device, including themes, keyboard/scrolling, and navigation. For shared semantics or realtime changes, change the same data from web and confirm mobile catches up within roughly 500ms, without manual refresh, including after reconnect.
- Test parsing/transforms in the existing Vitest setup. Preserve `scripts/ios-run.test.sh` coverage when changing the native build wrapper.
- Report which checks ran and which native/cross-client checks were unavailable. Do not claim visual or release verification from typecheck/unit tests alone.
