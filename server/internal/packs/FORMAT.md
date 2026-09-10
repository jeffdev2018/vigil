# Pack format (format_version 2)

A pack is one YAML file, `pack.yaml`, that turns a workspace into a ready-to-use
setup for a function: helpdesk, sales, HR… It is the workspace transfer bundle
(`workspace_transfer.go`) with a manifest on top and more kinds inside. A pack
works with zero agent ("human team first"); agents arrive as colleagues, paused
and without a runtime, and nothing in a pack ever carries a credential.

Everything is applied through the transfer pipeline: preview (collisions),
strategy (`rename` / `merge` / `skip`), one transaction, a report. Rows created
by a pack are recorded with the pack id and version, so a pack can be upgraded
or uninstalled.

## Rules for authors

- English content in `pack.yaml` (the product UI translates chrome, not packs).
  Titles, instructions, doctrine and sample issues are what the user reads:
  write them for a real team, concrete, no filler.
- Names are the identity: an agent, skill, label, status, project… is matched
  by `name` (or `key` when it has one). Keep them stable across versions.
- Agents: `trust_mode: propose` and `effect_mode: preview` unless the role
  genuinely needs more; `runtime_mode: native` so a workspace with the native
  runtime can run them at once; no `env_keys`; every agent lists the pack's
  procedure skill.
- Autopilots are imported **disabled** whatever the file says; say so in the
  description. Business rules are imported as **drafts**. Triage sources are
  imported without secrets.
- Statuses: only add what the function needs beyond the 7 built-ins
  (`backlog, todo, in_progress, in_review, done, blocked, cancelled`); each
  custom status names its `category` (one of those 7).
- Sample issues are examples and say so in their title or first line.
- Every pack declares its `metric`.

## File layout

```yaml
pack:
  id: helpdesk-it              # kebab-case, unique in the catalogue, immutable
  version: 1.0.0               # semver; bump on every change
  title: Internal helpdesk & IT
  summary: One sentence for the catalogue card.
  description: |
    Markdown. What the pack sets up, who it is for, how the first week goes.
  domain: helpdesk             # helpdesk | ops | support | sales | marketing | leadership | research | hr | finance | legal
  wave: 1                      # 1 | 2 | 3 (rollout wave, informational)
  author: Multica
  license: MIT
  tags: [it, tickets, sla]
  works_without_agents: true   # the pack is usable with no agent at all
  metric:
    label: First-response delay
    description: Median time between a ticket's creation and its first human or agent reply.
    hint: Issues of type "ticket", filter the view "Open tickets".
  prerequisites:               # declared, never enforced silently: the preview shows them
    - kind: native_runtime     # native_runtime | runtime | integration | channel
      name: Native runtime
      optional: true
      note: Lets the pack's agents run in the browser without a CLI.
    - kind: integration
      name: Slack
      optional: true
      note: Tickets can be filed from Slack.
  changelog:
    - version: 1.0.0
      note: First release.

# ---- Workspace configuration (new kinds, format_version 2) ----------------

issue_statuses:
  - key: waiting_on_requester  # optional, derived from name
    name: Waiting on requester
    description: The ticket cannot move until the requester answers.
    category: blocked          # backlog | todo | in_progress | in_review | done | blocked | cancelled
    color: "#f59e0b"

issue_types:
  - key: ticket
    name: Ticket
    description: A request from a colleague.
    color: "#3b82f6"
    icon: life-buoy            # lucide icon name

labels:
  - name: access               # unique per resource_type, case-insensitive
    description: Account and access requests.
    color: "#8b5cf6"
    resource_type: issue       # issue | project

properties:
  - name: Requester team
    type: select               # text | url | number | checkbox | date | select | multi_select | actor | multi_actor
    description: Which team filed the ticket.
    icon: users
    config:
      options:                 # select / multi_select only
        - { name: Sales, color: "#10b981" }
        - { name: Finance, color: "#f59e0b" }
    issue_types: [ticket]      # empty = every type

views:
  - name: Open tickets
    scope_type: workspace      # workspace | project (project views name `project`)
    project: ""                # project title when scope_type = project
    visibility: workspace
    query:                     # same keys as the app's saved views
      typeFilters: [ticket]
      statusFilters: [todo, in_progress, waiting_on_requester]
    display: {}                # display settings, optional

transition_rules:
  - from_category: in_review   # optional
    to_category: done
    allowed_roles: [owner, admin, member]
    allow_actor_types: [member]          # member | agent
    requires_approval: true
    approver_roles: [owner, admin]
    reject_status_key: in_progress
    enabled: true

business_rules:                # imported as drafts; a person activates them
  - title: No external reply without approval
    attach_point: issue_submit_review    # project_create | issue_submit_review | agent_run_dispatch | webhook_received
    natural_language: An issue labelled "customer-facing" cannot be submitted for review by an agent without a human approver.
    predicate: {}              # optional precompiled predicate; omitted = compiled at install when an LLM is configured, else kept as a draft to compile later
    action: {}

doctrine: |
  Markdown appended to the workspace doctrine as a section titled after the
  pack (or published as revision 1 when the workspace has none).

ownership_rules:
  - label: access              # label name (issue labels) — or path_pattern
    path_pattern: ""
    referent_agent: Helpdesk · Triage   # agent name in this pack, optional
    priority: 10

# ---- Kinds the transfer bundle already carries (same shapes) ---------------

permission_profiles:
  - name: Helpdesk read-only
    description: ...
    read_only: true
    denied_paths: []
    allowed_commands: []
    hidden_secrets: []

skills:
  - name: helpdesk-procedure
    description: How tickets are handled here.
    content: |
      Markdown procedure the agents follow.
    status: published
    config: {}
    files: []                  # [{path, content}]

agents:
  - name: Helpdesk · Triage
    description: Classifies and routes tickets.
    instructions: |
      Role, scope, what to never do, how to escalate.
    model: ""                  # empty = workspace default
    thinking_level: ""
    service_tier: ""
    runtime_mode: native       # native | local
    visibility: workspace
    max_concurrent_tasks: 1
    runtime_config: {}
    mcp_config: {}
    custom_args: []
    conversation_starters: []
    env_keys: []
    scoped_env_keys: []
    trust_mode: propose        # observer | propose | approval | autonomous
    effect_mode: preview
    permission_profile: Helpdesk read-only
    skills: [helpdesk-procedure]
    versions: []

projects:
  - title: Helpdesk
    description: ...
    icon: ""
    status: planned            # planned | in_progress | ...
    priority: medium
    start_date: ""
    due_date: ""
    resources: []
    goals: []                  # goal keys

goals:
  - key: g-first-response
    parent_key: ""
    title: Answer every ticket within a business day
    description: ...
    success_measure: ...
    due_date: ""
    status: active

autopilots:
  - title: Helpdesk · Daily stale-ticket sweep
    description: Disabled after install; enable it once an agent is connected.
    assignee_type: agent
    assignee_agent: Helpdesk · Triage
    execution_mode: create_issue      # create_issue | run_only
    issue_title_template: "Stale tickets · {{date}}"
    project: Helpdesk
    triggers:
      - kind: schedule                # schedule | webhook | api
        enabled: false
        cron: "0 9 * * 1-5"
        timezone: UTC
        label: ""
        provider: ""
        event_filters: null
        event_match_criteria: ""
        window_minutes: 0

triage_sources:
  - kind: email                # see triage_source kinds
    name: Helpdesk inbox
    icon: ""
    mode: manual
    auto_accept: null
    cap_per_hour: 0
    expiry_days: 0

org_structures:
  - project: Helpdesk
    model: hierarchy
    name: Helpdesk desk
    end_condition: ""
    budget_usd_ticks: 0
    definition:                # OrgDefinition JSON: units, edges, rules, committees (see org_catalog.go)
      units: []
      edges: []
      rules: []
      committees: []

notes:
  - title: Helpdesk · How we work
    content: |
      Markdown for the Brain.
    tags: [helpdesk]
    pinned: true

issues:
  - title: "Example · Laptop will not boot"
    description: ...
    status: todo
    priority: medium
    project: Helpdesk
    goal_key: ""
    labels: [hardware]
```

## Validation

`go test ./internal/handler -run TestBuiltinPacks` parses every embedded pack,
checks every enum above, every cross-reference (skills an agent lists, the
agent an autopilot names, projects, goal keys, labels, status keys, issue type
keys) and applies each pack to a fresh test workspace twice (the second apply
must be a no-op). Run it before committing a pack.
