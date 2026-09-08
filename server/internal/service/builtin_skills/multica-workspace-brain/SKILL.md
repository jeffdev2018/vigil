---
name: multica-workspace-brain
description: "Use when a run learns something durable about this workspace — a decision, a convention, a fact about the codebase, who owns what — or needs knowledge a previous run recorded. Not for run logs, task status, or anything true only today."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Workspace Brain

## Quick start

The Brain is the workspace's shared knowledge base. Every run receives it as files:

```
.multica/knowledge/README.md        index: title, tags, id, file name
.multica/knowledge/<slug>-<id8>.md  one note
```

Read the index first. Open only the notes you need.

```bash
multica brain list --search "release" --output json
multica brain show <note-id>
```

## What belongs in the Brain

Save something only if it will still be true and still be useful to the NEXT run.

Save:

- A decision and the reason behind it ("we dropped the queue table because the scheduler already leases; see MUL-2957").
- A convention nobody wrote down ("migrations run outside a transaction so CONCURRENTLY works").
- A hard fact about the codebase or the infrastructure ("the daemon reaches Postgres through pgbouncer on 6432").
- Ownership and contacts ("the release tag is cut by whoever owns the deploy pipeline").

Do not save:

- What you did in this task. That is the run's output, not knowledge.
- Anything that will be false next week: current branch names, in-flight PR numbers, "the build is red".
- Anything already in the repo. A note that restates `CLAUDE.md` is a second copy that will drift.
- Secrets, tokens, credentials, or personal data.

One note is one idea. A note nobody could act on without reading three others is too big.

## Before you save: look for the existing note

The Brain degrades when the same fact arrives four times under four titles.

```bash
multica brain list --search "<the key words of your fact>" --output json
```

If a note already covers it, UPDATE that note instead of adding a near-duplicate:

```bash
multica brain save --id <note-id> --content-file ./updated-note.md
```

Updating reads the note's current revision and sends it back. If someone edited it in
between, the write is refused with a conflict — re-read the note, merge, and retry.
Never work around a conflict by creating a second note.

## Saving

```bash
multica brain save \
  --title "Deploys go through the release tag" \
  --tags deploy,release \
  --content "Push v0.x.x on main; release.yml publishes the binaries and the Homebrew tap."
```

For anything longer than a line or two, write the body to a file first and pass it:

```bash
multica brain save --title "Scheduler leasing model" --tags backend,scheduler --content-file ./note.md
```

`--content-file -` reads stdin. Passing both `--content` and `--content-file` is an error,
not a preference.

Flags:

- `--title` — required for a new note. Max 200 characters, and it is what a human scans in
  a list: write the fact, not the topic. "Deploys go through the release tag" beats "Deploys".
- `--tags a,b` — lowercased, de-duplicated and sorted server-side. At most 10.
- `--pinned` — pin the note so EVERY run receives it regardless of age. Use it sparingly;
  pinned notes crowd out the recent ones.
- `--id <note-id>` — update an existing note instead of creating one.

Body is markdown, at most 20000 characters.

## Archiving

A note that became false is archived, not deleted:

```bash
multica brain archive <note-id>
```

Archived notes stop being injected into runs and leave the default listing, but stay
readable. Deleting is a human action in the Brain page — do not ask for it.

## What the run receives

Each run gets every pinned note plus the 20 most recently updated ones, capped at 200 KB
total. If the cap drops notes, `.multica/knowledge/README.md` says how many; find them with
`multica brain list`.

A daily curation pass merges near-duplicates, retitles vague notes, normalizes tags and
archives what has gone stale. Merged sources are archived with a pointer to the note they
were folded into, so nothing is lost. Write the note that is right today; the pass tidies.

## Boundaries

The Brain is workspace knowledge, shared by every agent. It is not:

- Your own agent memory (facts about how YOU work) — that is a different store.
- An issue, a comment, or a task report — those go through `multica issue` / `multica comment`.
- A postmortem — a failed run drafts one automatically.
