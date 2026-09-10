# Function packs

A pack is one file that turns a workspace into a working setup for a
function — helpdesk, ops, support, sales, marketing, leadership, research,
HR, finance, legal. It carries the work item types, statuses, properties,
views, transition and business rules, a doctrine section, projects, goals,
notes and agents that function needs, and it is applied through the same
transfer pipeline a workspace import uses: preview, one strategy, one
transaction, a report.

Every pack works with **no agent at all**. The types, views and rules are
what a human team uses on day one; the pack's agents arrive as paused
colleagues with no runtime bound, and a person connects them later. Nothing
in a pack ever carries a credential.

## You do not install packs

Installing, uninstalling, previewing, uploading and exporting a pack are
human-only writes: those endpoints require a person's own credentials and
answer **403** to a run's token, whatever the workspace role says. That is
deliberate — a pack rewrites the workspace's types, statuses, rules and
doctrine, which is not a change a run gets to make on its own.

What you can do is **read** the catalogue and the ledger, and tell the person
what installing something would do:

```bash
multica pack list --output json                  # catalogue + this workspace's install state
multica pack show helpdesk-it --output json      # manifest, metric, prerequisites, contents
multica pack installed --output json             # the install ledger
multica pack items <install-id> --output json    # every row one install created
```

If a task asks you to install, upgrade or remove a pack, say which command an
owner or admin runs and stop there. Do not work around the refusal by
creating the pack's types, labels and agents by hand: rows created that way
belong to nobody, so they cannot be upgraded or uninstalled, and the
workspace ends up with a half-pack nothing tracks.

## What a pack declares

| Group | Kinds |
|---|---|
| Work shape | `issue_statuses`, `issue_types`, `labels`, `properties`, `views` |
| Rules | `transition_rules`, `business_rules`, `ownership_rules`, `doctrine` |
| Agents | `permission_profiles`, `skills`, `agents` |
| Content | `projects`, `goals`, `autopilots`, `triage_sources`, `org_structures`, `notes`, `issues` |

Rows are matched **by name** (or by `key` where a kind has one), which is
what makes an upgrade possible: the same name in a later version is the same
row. Every pack also declares one `metric` — the single number the function
is judged on. When a run touches a pack's domain, that metric is the thing
worth moving.

Guarantees the install enforces whatever the file says: autopilots and their
triggers land **disabled**, business rules land as **drafts**, agents land
without `env_keys` and with `trust_mode: propose`, and triage sources land
without their token. So a freshly installed pack fires nothing until a person
turns each piece on.

## Preview, install, upgrade, uninstall

- **Preview** reports the collisions (a row of the same name already in the
  workspace), any validation problems, the strategy that would be used and a
  `blocked` reason when the install would be refused.
- **Strategy** is `skip` by default on a first install — a pack never
  overwrites what the workspace already has — and `merge` on an upgrade, so
  the pack's own rows follow the new version. `rename` keeps both.
- **Upgrade** is installing a newer version of the same pack id. Re-installing
  the version already installed is refused (`409`) unless the person passes
  `--force`; a lower version is always refused.
- **Uninstall** removes the *configuration*: agents and autopilots are
  archived, rules, views, labels, properties, statuses and types are removed
  when nothing uses them. **Content stays** — projects, goals, notes, issues,
  and anything the pack merged into a row that already existed. The report
  lists what was kept and why, so "the pack is gone but the type is still
  there" is an answer, not a bug.

The **ledger** is what makes all of that work: each install records every row
it created or merged, with the pack id and version. `multica pack items
<install-id>` is that list. A row marked `merged` is never deleted by an
uninstall — it was the workspace's row before the pack touched it.

`prerequisites` on a pack are declared, never enforced: the preview marks
each one `met`, `missing` or `unknown`. A pack whose native-runtime
prerequisite is missing still installs — its agents simply have nothing to
run on yet.

## Endpoints

Workspace-scoped through `X-Workspace-ID`. Read for any member, write for an
owner or admin **who is a person**.

| Endpoint | What |
|---|---|
| `GET /api/packs` | Catalogue: manifest, counts, install state, upgrade flag, prerequisite status |
| `GET /api/packs/{id}` | One pack with its contents per kind |
| `GET /api/packs/{id}/download` | The pack file itself |
| `GET /api/packs/installed[/{id}]` | The ledger, or one install with its rows |
| `POST /api/packs/{id}/preview\|install` | Human only — preview or apply a catalogue pack |
| `POST /api/packs/preview\|install` | Human only — the same for an uploaded pack file |
| `POST /api/packs/installed/{id}/uninstall` | Human only — remove the configuration |
| `POST /api/packs/export` | Human only — this workspace's configuration as a pack file |

Installs and uninstalls are audited (`pack.installed`, `pack.uninstalled`)
and broadcast, so an open page updates without a reload. A workspace can also
be created straight from a pack, by passing its id as `pack_id`.

The format itself is one YAML file. `multica pack download <id>` gives you a
real one to read, and the product documentation's Packs page states the rules
an author follows and the format's known limits.

Exports never carry skills discovered on a connected computer (`runtime_local`); `multica pack export --include-skills=false` leaves every skill out. The web app uploads packs up to 8 MB; larger files go through `multica pack install-file`.
