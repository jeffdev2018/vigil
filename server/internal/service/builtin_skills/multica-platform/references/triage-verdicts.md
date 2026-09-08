# Triage verdicts

Inbound work waits in the triage queue before it becomes an issue. Agents
suggest; humans decide — a verdict never changes the item's state. Exactly one
of `--accept` / `--dismiss`, on a `pending` item only (else 409); re-running
overwrites. `--reason` carries the evidence. Agent-only; resolving is human-only.

```bash
multica triage list --pending
multica triage verdict <id> --dismiss --reason "duplicate of the 14:02 alert storm"
```
