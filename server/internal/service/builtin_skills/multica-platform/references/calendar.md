# The workspace calendar

Events with participants who are members or agents. A member schedules; an
agent **proposes** — the event is filed as *proposed* on the issue with a
Decision Card, and a person accepts or declines. Nothing an agent files
lands on anyone's calendar without that click.

## Tools

- Native runtime: `list_events {from?, to?, agenda?}`, `find_slot
  {participants, duration_minutes?, from?, to?, tz?}`, `propose_event
  {title, starts_at, ends_at, description?, timezone?, location?,
  participants?}` (on this run's issue).
- MCP: the `vigil_calendar` compound tool — actions `events`, `agenda`,
  `slots`, `propose` (granular names `calendar_events`, `calendar_agenda`,
  `calendar_slots`, `calendar_propose`).
- CLI: `multica calendar agenda|events|slots|propose`, `--from`/`--to` RFC
  3339 (default a week ahead), `--output json` for the full rows.

`agenda` is the wider read: events plus issues due, cycles and meetings in
the window. `slots` returns the first free windows every listed participant
can make — members inside 09:00–18:00 on weekdays in `tz`, agents outside
their events at any hour. Participants are `member:<user id>` or
`agent:<agent id>`; ids come from `vigil_team` / `multica member list`.

## How to propose well

1. Read the agenda first: do not propose over an issue's due date or on top
   of an event the people already have.
2. Ask `slots` with everyone who must attend and a realistic duration; pick
   from what comes back rather than inventing a time.
3. Give the proposal a title that says what will be decided, a description
   that says why now, and the participants who must be there. Times are RFC
   3339; say the timezone people read them in.
4. Then stop. The card carries the proposal; a person accepts or declines.
   If your run is still going, `list_events` shows the status (`proposed`,
   `scheduled`, `cancelled`); do not file the same proposal twice.

Every proposal is one effectful action against the run's budget. An
imported Google event (`source: google`) belongs to the member who imported
it; propose next to it, never on top of it.
