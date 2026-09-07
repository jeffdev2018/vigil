# Epic step runs

If your brief asks you to write a PRD, a technical plan, a wireframe or a
ticket breakdown for a project, you are on an **epic step run**. It is
read-only: change nothing, run nothing, open nothing. The whole deliverable is
one answer.

## What the pipeline is

A project's epic is four artifacts in a fixed order:

`prd` → `tech_plan` → `wireframe` → `tickets`

Each one is written, reviewed by a human, and approved. **Only an approved step
unlocks the next one**, so your brief only ever asks for a step whose
predecessor is already approved — and it hands you that approved text. Write
inside it. A tech plan that contradicts the approved PRD is a step backwards:
the human has to reopen the PRD, which supersedes every step after it.

Your answer is a **draft**. It is never approved by writing it, and nothing it
says creates an issue. Only a human pressing Approve, and then Apply on the
tickets step, turns your breakdown into real issues.

## Answer format

End with **exactly one** fenced block. Nothing else in your answer is read.

````
```epic_step
{"kind":"prd","content":"# Problem\n…markdown…","payload":{}}
```
````

Fields:

- `kind` — the step you were asked for: `prd`, `tech_plan`, `wireframe` or
  `tickets`. A block naming a different step than the brief asked for is
  discarded, not filed under the step you named.
- `content` — Markdown. **Must not be empty**: an empty block is read as a
  failed run and the step is offered for regeneration.
- `payload` — structured data. `{}` for every step except `tickets`.

## Per step

**`prd`** — the problem, who has it, what success looks like, what is in scope
and what is explicitly out of it.

**`tech_plan`** — the approach, the components it touches, the data it changes,
the risks, and the order of work. Stay inside the approved PRD's scope.

**`wireframe`** — describe the screens as **text**. An ASCII box layout or a
Mermaid diagram inside a fenced block in `content`. Images are not supported
and a link to one is not a wireframe.

````
```epic_step
{"kind":"wireframe","content":"```mermaid\nflowchart LR\n  List --> Detail\n  Detail --> Edit\n```\n\n### List\n+----------------------+\n| Search        [ + ] |\n| ○ Row               |\n+----------------------+"}
```
````

**`tickets`** — break the approved plan into independently shippable pieces.
The list goes in `payload.tickets`:

````
```epic_step
{"kind":"tickets","content":"# Breakdown\nThree tickets, T2 and T3 wait on T1.","payload":{"tickets":[
  {"external_key":"T1","title":"Add the endpoint","description":"…","depends_on":[]},
  {"external_key":"T2","title":"Test it","description":"…","depends_on":["T1"]},
  {"external_key":"T3","title":"Document it","description":"…","depends_on":["T1"]}
]}}
```
````

- `external_key` — yours to choose, unique inside this list. It is how a
  re-applied breakdown recognises the tickets it already created, so **keep a
  ticket's key stable across revisions**: changing it creates a second issue
  for the same work.
- `title` — required, one line, what changes.
- `description` — what changes and how it is verified.
- `depends_on` — the `external_key` of every ticket that must land first. It
  becomes a blocking link between the real issues. A key that is not in the
  list, a ticket depending on itself, or a cycle rejects the whole breakdown.
- At most **50** tickets. A breakdown past that is wrong, not large.
