# Activation self-pilot — 7 septembre 2026

## Chromium harness (re-run)

```bash
node docs/research/activation-readiness-preview-2026-09-07.cjs
```

Captures refreshed in `/tmp/vigil-activation/`: `blocked-desktop.png`, `blocked-phone.png`, `ready-hidden-desktop.png`. `phoneMetrics.width=390`, `errors=[]`.

## Operator path to first agent result

Environment already warm (API+daemon online, Claude CLI present). From wiring the pilot project/agent/issue to first completed agent run (**DEV-1**): **~4 minutes**. Delivery Accept ~2 minutes later.

This is a **self-pilot on a prepared machine**, not an external-team cold start timed under 10 minutes. Telemetry event `activation_checklist_viewed` was not separately asserted in this pass.
