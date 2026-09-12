---
name: multica-workspace-brain
description: "Use when a run learns something durable about this workspace — a decision, a convention, a fact about the codebase, who owns what — needs knowledge a previous run recorded, or wants to capture a lead it is unsure about. Not for run logs, task status, or anything true only today."
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
multica brain search "release tag" --output json   # ranked: best match first
multica brain show <note-id>
```

`search` is how you look for something; `list` is how you browse. Search ranks by
relevance and returns, per hit, a score, the section of the note that matched
(`passage_heading`) and a snippet from it. Ask in plain words or a whole question, in
any language: accents are optional, Chinese, Japanese and Korean words match inside
longer text, and a note needs only half of the words to rank. `"a quoted phrase"` must
appear as written and `-word` excludes; with either, every word is required. When the
workspace has an embeddings model configured, the ranking also finds notes that word
the same fact differently — the output says so when it does not.

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
multica brain search "<the key words of your fact>" --output json
```

If a note already covers it, UPDATE that note instead of adding a near-duplicate:

```bash
multica brain save --id <note-id> --content-file ./updated-note.md
```

Updating reads the note's current revision and sends it back. If someone edited it in
between, the write is refused with a conflict — re-read the note, merge, and retry.
Never work around a conflict by creating a second note.

## Not sure it belongs? Capture it instead

`save` writes a note EVERY later run reads. `capture` parks the thing in the Brain's
capture inbox, where a person turns it into a note, merges it into an existing one, or
discards it. Nothing is filed behind anyone's back.

Capture when you are unsure — a link worth reading, a remark that might be a convention,
a lead. Save only when you are sure it is durable workspace knowledge and you have
already searched.

```bash
multica brain capture "the daemon reaches Postgres through pgbouncer on 6432"
multica brain capture --url https://example.com/locking --title-hint "Locking article"
echo "a longer thought" | multica brain capture
multica brain capture --file ./whiteboard.png       # or a voice memo, or any document
```

The kind (text, link, todo) is inferred; `--kind` overrides it for typed captures, and
never applies to `--file` — there the file's content type decides. A voice memo is
transcribed when the workspace has speech-to-text configured; the transcript lands on
the capture as its text.

A wrong capture costs someone one click. A wrong note pollutes every run that follows.
When in doubt, capture.

## Working the inbox

```bash
multica brain inbox                          # what is still raw, newest first
multica brain inbox --status all --limit 50
multica brain suggest <capture-id>           # what a model proposes, if one is configured
```

Filing a capture is one command, and only works while the capture is raw:

```bash
multica brain organize <capture-id> --as note --title "Deploys go through the release tag" --tags deploy
multica brain organize <capture-id> --as merge --note <note-id>
multica brain organize <capture-id> --as discard
multica brain reopen <capture-id>            # a discard you want back
```

`--as note` defaults the title to the capture's title hint, then its first line, and the
body to the capture rendered as markdown. `--as merge` appends to an existing note and
unions the tags — that is the right move whenever a note already covers the subject.

Organizing someone else's capture is filing their thought under your words. Do it when
the capture is unambiguous or when you were asked to; otherwise leave it raw and say what
you would have done.

`multica brain delete <capture-id>` removes a capture and its file for good. Discard,
which is reversible, is almost always the right answer instead — delete only when a
person asked for it gone.

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
