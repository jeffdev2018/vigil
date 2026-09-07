# Work item types

A work item type says what an issue IS — bug, story, epic, task, or whatever
else the workspace defined. It is a per-workspace catalogue, and every issue
carries at most one type key.

## What a type is NOT

A type carries **no platform behavior**. It starts no run, changes no status,
gates no transition and blocks no assignment. Everything a type does is
classification: it groups work, it filters, and it decides which custom
properties apply to the issue.

That is the difference from a status, and it matters when you are deciding what
to write: moving an issue to a `todo`-category status starts the assigned agent,
while classifying the same issue as a `bug` starts nothing. If you want work to
begin, change the status.

## Untyped is a real state

`issue_type` is `null` on an issue nobody classified — which is every issue
created before types existed, and every issue created without naming one. An
untyped issue is fully usable: it lists, it filters, it runs, it closes.

Do not classify an issue just because the field is empty. Set a type when the
classification is something you actually know.

## The four seeded types

Every workspace starts with `bug`, `story`, `epic` and `task`. Those four
**cannot be archived** — the handles are documented and referenced — though a
workspace may rename or recolor them, so never assume `bug` is displayed as
"Bug". Read the catalogue when you need a label; write the key.

A workspace may define custom types beside them. Custom types can be archived,
which retires them from future assignment and leaves every issue already on one
exactly as it is.

## Reading the catalogue and writing a type

The catalogue is a workspace read: `GET /api/issue-types` returns every type,
archived included, with its `key`, `name`, `color` and `is_system`.

Set an issue's type by writing the KEY, not the display name:

```
multica issue update <issue-id> --type bug
multica issue update <issue-id> --type ""     # back to untyped
```

Over the API the same write is `PUT /api/issues/{id}` with
`{"issue_type": "bug"}`, and `null` clears it.

A key the catalogue
does not know is refused with `400 unknown_issue_type`; an archived key is
refused with `400 archived_issue_type`. Neither is retryable — read the
catalogue and pick a key that exists.

## Types decide which properties apply

A custom property is either **global** or **scoped to some types**.

- A global property (no scope) applies to every type AND to untyped issues.
- A scoped property applies only to issues carrying one of its types. It does
  NOT apply to an untyped issue: "no type" matches no type list.

Writing a value for a property that does not apply is refused with
`409 property_not_applicable`. That is a state problem, not a payload problem:
the fix is to classify the issue with a type the property covers, or to leave
the value unwritten — never to retry the same write.

## Changing a type never deletes a value

Reclassifying an issue keeps every property value it already has. Values that
the new type does not cover become hidden rather than deleted, and they come
back intact if the issue is classified back. So a type change is safe to make
and safe to undo, and you never need to copy values out first.

The corollary: a hidden value is still there. If you are reading an issue to
answer a question, an out-of-scope value is real data, not a leftover.
