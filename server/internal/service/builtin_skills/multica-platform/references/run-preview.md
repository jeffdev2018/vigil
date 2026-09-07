# Run previews

A run in worktree mode can bring up the app it is working on, and a reviewer
can open it from a browser — including a phone — without installing anything
or opening a port.

- [How a run gets a preview](#how-a-run-gets-a-preview)
- [The port block](#the-port-block)
- [Where the URL appears](#where-the-url-appears)
- [What a preview cannot do](#what-a-preview-cannot-do)

## How a run gets a preview

The project's `local_directory` resource carries three lifecycle scripts, each
an argv, never a shell string:

| Script | When | Failure |
|---|---|---|
| `setup` | once the worktree exists, before the agent | fails the run |
| `run` | after `setup`, alongside the agent | the preview is marked `error`, the run continues |
| `archive` | before the worktree is delivered | logged only |

`run` is the one that starts the dev server. It is the only long-lived one: it
stays up for the whole run and is killed, with its whole process group, when the
run ends. A worktree run whose resource declares no `run` script simply has no
preview, which is the normal case.

Declare it in the project's local directory settings, next to `setup`. Since it
is an argv, a pipeline goes in a script file that is named here instead:

```
run: ["npm", "run", "dev"]
run: ["./scripts/dev.sh"]
```

The dev server must bind **127.0.0.1**, not `0.0.0.0`: nothing needs it on the
network, and the relay reaches it on loopback.

## The port block

Every run is given a private, contiguous block of TCP ports so two concurrent
runs on the same repository never collide:

- `MULTICA_PORT_BASE` — the first port of this run's block
- `MULTICA_PORT_COUNT` — how many ports it owns

The `run` script MUST bind `MULTICA_PORT_BASE`. That is the port that is probed,
declared, and relayed; a server that hardcodes 3000 gets no preview, and on a
second concurrent run it collides with the first.

```
run: ["sh", "-c", "npm run dev -- --port $MULTICA_PORT_BASE"]
```

The probe waits up to ninety seconds for the port to answer, so a first compile
has time. Any response below `500` counts as up, including a `404` — a
single-page app that has not mounted its router yet is still a running server.

## Where the URL appears

The preview shows up as a chip on the run row, and in full on the run's detail
panel: the address, a copy button, and the controls for sharing it.

Two shapes, and the difference matters:

- **Relayed.** The server can proxy requests to the run's machine over the
  connection the daemon already holds open. The reviewer gets a public link,
  scoped to this run and expiring, that works from any browser. Revoking it
  stops serving on the very next request.
- **Local.** The server cannot relay — the machine's daemon is too old, or the
  deployment turned relaying off. The address is `http://127.0.0.1:<port>`, and
  it only means anything on the machine that ran the task. The desktop app opens
  it; the web UI says it is local instead of handing out a link that cannot work.

A preview reports `starting`, `ready`, `stale`, `stopped` or `error`. Only
`ready` has an address. `error` carries the tail of the `run` script's log,
which is the thing that says what to fix.

## What a preview cannot do

- **It does not outlive its run.** When the run ends the process group is killed
  and the preview becomes `stopped`; an open link answers "gone" rather than
  silently going nowhere.
- **No WebSockets.** Hot module reload does not survive the relay. Reload the
  page.
- **No click-to-source.** The preview is the app, not an editor.
- **Responses are capped.** A very large asset is truncated rather than refused.
