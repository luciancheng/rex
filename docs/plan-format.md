# Plan format v1

A plan is one unit of work (a feature, a fix, a migration) saved as JSON in the repo. Anyone who never saw the chat should be able to open it and see what the work is for, where its edges are, how it breaks into PR-sized tasks, what blocks what, and what was actually done.

The format is frozen at `"version": 1`. Rex ignores fields it does not know, so tools may add their own, but Rex never reads or writes anything outside this page.

## Files

- JSON only, named `<stem>.rex.json`. The stem can't be empty.
- They live in `rex/plans/` at the repo root (the folder that holds `.git`). Subfolders group plans: `rex/plans/onboarding/auth.rex.json`.
- Commit the folder. Git is how a team shares plans.
- Creating a plan never overwrites a file that already has that name.
- UTF-8, with or without a BOM. Rex keeps your spacing, key order, and line endings when it edits a file.

## Example

```json
{
  "version": 1,
  "id": "file-storage",
  "title": "Plan file storage and navigator",
  "goal": "People can create, open, and share plans under rex/plans.",
  "status": "active",
  "scope": {
    "in": ["JSON plan format", "folder browser"],
    "out": ["agent auto-loads the plan", "extra palettes"]
  },
  "tasks": [
    {
      "id": "plans-ui",
      "title": "Plans navigator",
      "status": "done",
      "does": ["Create, open, browse rex/plans in the TUI"],
      "accomplished": ["Home cards, browse, duplicate warn"],
      "modified": ["internal/tui/plans.go"],
      "verified_how": ["go test ./internal/tui/ and a tmux walk of Create → Open"]
    },
    {
      "id": "card-highlight",
      "title": "Selected plan card is one block",
      "parent": "plans-ui",
      "status": "active",
      "depends_on": ["plans-ui"],
      "needs": ["pick / pickbg colors exist"],
      "does": ["Bar, border, and fill cover the whole selected card"],
      "verify": ["Open latest: ▌ on all three card rows"],
      "files": ["internal/tui/plans.go", "internal/tui/live.go"]
    }
  ]
}
```

## Plan fields

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | integer | `1`. A missing version reads as 1. A higher one makes the file read-only in Rex. |
| `id` | string | Slug for the plan. New plans take it from the file name. |
| `title` | string | Short name. |
| `goal` | string | One paragraph: what this send-off wants to accomplish. |
| `status` | status | The plan as a whole. |
| `scope.in` | list | The hard edges: this is the job. |
| `scope.out` | list | The hard edges: this is not the job. Drift checks use this list. |
| `tasks` | list of tasks | Flat, in reading order. |

## Task fields

| Field | Type | Written by | Meaning |
| --- | --- | --- | --- |
| `id` | string | planner | A stable slug (`card-highlight`). Not a UUID, not an index. Unique in the file. |
| `title` | string | planner | Short name. |
| `parent` | id | planner | **Outline**: this task sits under that heading. |
| `status` | status | planner, Rex | See below. |
| `scope.in` / `scope.out` | lists | planner | Narrower edges than the plan's. |
| `needs` | list | planner | Preconditions: facts, blockers, "must already exist". Not the work itself. |
| `does` | list | planner | Intended outcome: what will be true when this task is done. |
| `description` | string | planner | Longer intent, if the title and `does` are not enough. |
| `depends_on` | list of ids | planner | **Order**: do not start until those are done. May cross branches of the outline. |
| `verify` | list | planner | How a stranger would check it: commands or observations. |
| `files` | list | planner | Likely repo-relative paths or globs. Hints, not a lock. |
| `accomplished` | list | on completion | What actually happened. A retrospective, not a copy of `does`. |
| `modified` | list | on completion | Paths actually touched. |
| `verified_how` | list | on completion | How it was actually checked. |
| `completed_notes` | string | on completion | Anything the next send-off should know. |

Keep `needs` and `does` apart: one is "we still lack X", the other is "we will ship X". Merged into one description, you can no longer tell them apart.

**Types are forgiving on read.** A list field also accepts a single string (one item) or `null`. A text field written as a list of strings is read one item per line. Empty strings are skipped.

## Status

`pending` · `active` · `done` · `blocked` · `skipped` (case-insensitive). A missing status is `pending`; an unknown one is reported and read as `pending`.

- Write status down. Do not leave it to be inferred from git or from `verify`.
- At most one task is `active`: that is the pinned task. If none is active, the pin is the first `pending` task, in file order, whose `depends_on` are all `done` or `skipped`.
- Only leaf tasks (tasks that are nobody's `parent`) are pinned. A parent's progress is its children's, and Rex never writes a parent's status.
- A `depends_on` that names a parent is met once every leaf under it is `done` or `skipped` (or the parent itself is marked `done` or `skipped`). A task under that parent never waits on itself: only the parent's other leaves count.

## Parent vs depends_on

`parent` is where a task sits in the outline. `depends_on` is when it can start. They are independent: a task can sit under "UI" and depend on a task under "storage".

Keep the `tasks` array flat. Do not nest `children` objects: nested trees make git diffs and cross-references painful. If the outline is deeper than two or three levels, split it into another plan file.

## Size

Each task should be one PR that a human can review in a reasonable sitting. If it isn't, split it. Rex cannot enforce this; it is a habit.

## Who writes what

- **Agents and humans** write the plan: every planning field, and statuses as work moves.
- **Rex** writes only when you act in Rex: pinning sets `status`, and marking a task done writes `status` plus `accomplished`, `modified` and `verified_how` (and `completed_notes` if you typed one). Rex edits only the members it changes, inside that task's object: a completion field changes only when you edit it in Rex, and `modified` only gains paths.
- **Agents finishing a task** end their final reply with a trailer, so Rex can prefill the completion:

  ```
  accomplished: <what actually happened, one line>
  verified: <how it was checked, one line>
  ```

## Never in a plan

- Agent prompts, system prompts, or "nudge" text
- Auto-approve lists or tool permissions
- Diffs or file bodies
- Secrets
- Timestamps that change on every save (git blame already has them)

## What Rex does with a file it can't fully trust

Rex shows what it can and lists the rest under the plan:

- duplicate, missing, or non-string task ids;
- a `parent` or `depends_on` that names no task;
- loops in `depends_on` or `parent`;
- more than one `active` task;
- an unknown status (read as `pending`);
- a value of the wrong type (ignored).

Rex will not write to a file whose `version` is newer than 1 or unreadable (a quoted number such as `"1"` is read, with a warning), whose `tasks` is not a list, or that still has git conflict markers. A missing `version` reads as 1. A file that is not valid JSON or that has a duplicate key is not shown: Rex keeps the last good copy on screen and names the line and column (`14:3`); for conflict markers it names the line of the first one.

## A new plan

Creating a plan writes every plan field, empty, ready for an agent or a human to fill:

```json
{
  "version": 1,
  "id": "auth-flow",
  "title": "",
  "goal": "",
  "status": "pending",
  "scope": {
    "in": [],
    "out": []
  },
  "tasks": []
}
```

An old `{}` plan is still valid: it is a plan with no tasks.
