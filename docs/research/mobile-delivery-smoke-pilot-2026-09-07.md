# Mobile delivery smoke — pilot note 7 septembre 2026

Checklist: [mobile-delivery-smoke-checklist-2026-09-07.md](mobile-delivery-smoke-checklist-2026-09-07.md).

## Done without simulator

- Automated: `apps/mobile` delivery schema/key tests — **6/6 pass**.
- Backend path exercised end-to-end on **DEV-1** (same APIs mobile uses): criteria → completed run → Accept with assessments; honesty contract unchanged (`authorization-frontiers` / security-model).
- Copy-faithful **390px** HTML capture of mobile honesty chrome (strings taken from `apps/mobile/components/issue/issue-delivery-section.tsx`): `/tmp/vigil-mobile-delivery-smoke/honesty-phone-390.png`.

## Blocked

- **iOS Simulator / device captures** of the real Expo UI: this machine has `/Library/Developer/CommandLineTools` only — `xcrun simctl` unavailable; no Xcode.app. Cannot honestly tick checklist items 1–6 as “seen on sim”.

## Next to close for real

1. Install Xcode (or use a Mac with sim) / physical device.
2. `cd apps/mobile && pnpm ios` against `vigil-482` API.
3. Walk checklist on DEV-1 (or a fresh issue); store screenshots under `/tmp/vigil-mobile-delivery-smoke/`.
