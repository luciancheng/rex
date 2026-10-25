# Plan format v1

A plan is one unit of work (a feature, a fix, a migration) saved as JSON in the repo. Anyone who never saw the chat should be able to open it and see what the work is for, where its edges are, how it breaks into PR-sized tasks, what blocks what, and what was actually done.

The format is frozen at `"version": 1`. Rex ignores fields it does not know, so tools may add their own, but Rex never reads or writes anything outside this page. Optional fields added to this page (`base`, `branch`) keep version 1: a Rex that predates them ignores them.

## Files

- JSON only, named `<stem>.rex.json`. The stem can't be empty.
- They live in `rex/plans/` at the repo root (the folder that holds `.git`). Subfolders group plans: `rex/plans/onboarding/auth.rex.json`.
- Commit the folder. Git is how a team shares plans.
- Creating a plan never overwrites a file that already has that name.
- UTF-8, with or without a BOM. Rex only reads plan files (apart from writing a new empty one, below); an agent editing one keeps every byte it doesn't change.

## Example

```json
{
  "version": 1,
  "id": "file-storage",
  "title": "Plan file storage and navigator",
  "goal": "People can create, open, and share plans under rex/plans.",
  "status": "active",
  "base": "dev",
  "scope": {
    "in": ["JSON plan format", "folder browser"],
    "out": ["agent auto-loads the plan", "extra palettes"]
  },
  "tasks": [
    {
      "id": "plans-ui",
      "title": "Plans navigator"
    },
    {
      "id": "plans-browse",
      "title": "Create, open and browse plans",
      "parent": "plans-ui",
      "status": "done",
      "does": ["Create, open, browse rex/plans in the TUI"],
      "branch": "feature/plans_browse",
      "accomplished": ["Home cards, browse, duplicate warn"],
      "modified": ["internal/tui/plans.go"],
      "verified_how": ["go test ./internal/tui/ and a tmux walk of Create → Open"]
    },
    {
      "id": "card-highlight",
      "title": "Selected plan card is one block",
      "parent": "plans-ui",
      "status": "active",
      "depends_on": ["plans-browse"],
      "needs": ["pick / pickbg colors exist"],
      "does": ["Bar, border, and fill cover the whole selected card"],
      "verify": ["Open latest: ▌ on all three card rows"],
      "files": ["internal/tui/plans.go", "internal/tui/live.go"],
      "branch": "feature/card_highlight"
    }
  ]
}
```

## Plan fields

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | integer | `1`. A missing version reads as 1. A higher one is shown read-only, a warning for whoever edits it. |
| `id` | string | Slug for the plan. New plans take it from the file name. |
| `title` | string | Short name. |
| `goal` | string | One paragraph: what this work wants to accomplish. |
| `status` | status | The plan as a whole. |
| `base` | string | Optional. The branch the plan's stack of PRs lands on: the first leaf task's PR targets it. Usually the trunk (`main`, `dev`); the top branch of another stack when this plan builds on one. See [Stacks](#stacks). |
| `scope.in` | list | The hard edges: this is the job. |
| `scope.out` | list | The hard edges: this is not the job. Drift checks use this list. |
| `tasks` | list of tasks | Flat, in reading order. |

## Task fields

| Field | Type | Written by | Meaning |
| --- | --- | --- | --- |
| `id` | string | planner | A stable slug (`card-highlight`). Not a UUID, not an index. Unique in the file. |
| `title` | string | planner | Short name. |
| `parent` | id | planner | **Outline**: this task sits under that heading. |
| `status` | status | planner, and the agent working it | See below. |
| `scope.in` / `scope.out` | lists | planner | Narrower edges than the plan's. |
| `needs` | list | planner | Preconditions: facts, blockers, "must already exist". Not the work itself. |
| `does` | list | planner | Intended outcome: what will be true when this task is done. |
| `description` | string | planner | Longer intent, if the title and `does` are not enough. |
| `depends_on` | list of ids | planner | **Order**: do not start until those are done. May cross headings of the outline. In a stack, earlier tasks only. |
| `verify` | list | planner | How a stranger would check it: commands or observations. |
| `files` | list | planner | Likely repo-relative paths or globs. Hints, not a lock. |
| `branch` | string | planner, or the agent that cuts it | Optional. The head branch of this leaf task's PR. See [Stacks](#stacks). |
| `accomplished` | list | the agent, on completion | What actually happened. A retrospective, not a copy of `does`. |
| `modified` | list | the agent, on completion | Paths actually touched. |
| `verified_how` | list | the agent, on completion | How it was actually checked. |
| `completed_notes` | string | the agent, on completion | Anything the next chat should know. |

Keep `needs` and `does` apart: one is "we still lack X", the other is "we will ship X". Merged into one description, you can no longer tell them apart.

**Types are forgiving on read.** A list field also accepts a single string (one item) or `null`. A text field written as a list of strings is read one item per line. Empty strings are skipped.

## Status

`pending` · `active` · `done` · `blocked` · `skipped` (case-insensitive). A missing status is `pending`; an unknown one is reported and read as `pending`.

- Write status down. Do not leave it to be inferred from git or from `verify`.
- At most one leaf task is `active`: that is the pinned task, the one being worked. (A parent marked `active` is only a label.) If none is active, the pin is the first `pending` task, in file order, whose `depends_on` are all `done` or `skipped`.
- Only leaf tasks (tasks that are nobody's `parent`) are pinned. A parent's progress is its children's, and nobody writes a parent's status.
- A `depends_on` that names a parent is met once every leaf under it is `done` or `skipped` (or the parent itself is marked `done` or `skipped`). A task under that parent never waits on itself: only the parent's other leaves count.

## Parent vs depends_on

`parent` is where a task sits in the outline. `depends_on` is when it can start. They are independent: a task can sit under "UI" and depend on a task under "storage".

Keep the `tasks` array flat. Do not nest `children` objects: nested trees make git diffs and cross-references painful. If the outline is deeper than two or three levels, split it into another plan file.

## Size

Each task should be one PR that a human can review in a reasonable sitting. If it isn't, split it. Rex cannot enforce this; it is a habit.

## Stacks

A plan can be one stack of pull requests: each PR's base branch is the head branch of the PR below it, and the bottom one targets the trunk. A plan is a stack when it names a `base` or a task names a `branch`. A plan with neither is not a stack, and nothing in this section applies to it.

- **Each leaf task is one PR.** A parent is a heading, never a PR; a `branch` on it is ignored. `skipped` tasks have no PR, but their `branch` stays taken (its commits may still be there).
- **File order is stack order.** The first leaf's PR targets `base` (without one, the repo's base branch). Each next leaf's PR targets the branch of the leaf before it. So each task builds and passes its tests on the tasks before it alone, and `depends_on` names only earlier tasks.
- **One plan, one stack.** Tasks that need none of each other's code still stack, in the order they should land. Work that must merge on its own goes in its own plan. A plan that builds on another unmerged stack names that stack's top branch as `base`.
- **Branch names are plain:** letters, digits and `. _ - /`, at most 80 bytes, not starting with `-`, `.` or `/`, not ending with `.` or `/`, with no `..`, `//` or `.lock` part, and not `HEAD`. Two branches (or a branch and the base) can't nest (`feature/a` and `feature/a/b`), since git can't hold both. Agents put them into git commands, so Rex reports any other name. One branch per task, never the base. Name them the way the repo does.
- **Rex never runs git or talks to a forge.** It reads `base` and `branch` and never writes them. Where the pinned task's PR goes follows from the file alone, and the agent working it works it out that way (the `rex-plan` skill teaches it): its branch is the task's `branch`; its base is the branch of the nearest earlier leaf that is `done`, else `base`. Earlier leaves that are not done yet join under it when they are. Later leaves that are already `done` are PRs that sit on it, rebased onto it after a fix. Whether a PR has merged is the forge's business: once the PR below has merged, the base is `base`.
- **One plan for the whole stack.** Rex reads the plan from the working tree, so it must stay the same file whichever branch is checked out. Commit it only where every stack branch will hold it: on the trunk before the first branch is cut (a plan PR merged and the trunk pulled first), or once the stack has landed. While the stack is in flight, never commit it on any branch: every branch then holds the same copy (or none), so its changes in the working tree follow every `git switch` and `git rebase --autostash`. Until the stack lands, its progress lives in the checkout Rex runs in. The agent working a task writes its `branch` if the plan had none.

## Who writes what

- **Agents and humans** write the plan: every planning field, and statuses as work moves.
- **The agent working a task** writes its progress: it sets the task's `status` to `active` when it starts (and any other `active` leaf to `pending`, and the plan's own `status` to `active` if it was `pending` or `done`), in a save before any other edit, and when it finishes fills `accomplished`, `modified`, `verified_how` and `completed_notes`, then sets it `done` (in one save if it can); the plan's own `status` becomes `done` after that, once every leaf task is `done` or `skipped`. It edits only the members it changes, inside the task objects it changes (plus the plan's own `status`), and never a parent's status. It re-reads the file at the start of every turn while it works the plan, since someone else may have changed it.
- **`base` and `branch`** are planning fields: the planner writes them, and the agent working a task may write its own task's `branch`. Rex reads them and never writes them.
- **Rex writes nothing** to a plan but a new empty stub you create in its plan list ([A new plan](#a-new-plan)), never over an existing file. It reads the file, shows it live, and judges drift against the pin; you ask your agent to pin, finish or continue a task, and Rex shows the change when the file does. Its outline shows the pinned task's `does`, `needs`, `verify` and `files`, and the `accomplished` of the done task it builds on, on a card: what a fresh chat reads from the file to pick the task up. Before each prompt, Claude Code's and Codex's hooks hand the agent the same fields of the task that is `active`, in the plan Rex last opened in the repo, as a short note read from the file (docs/ux.md §7a); a task that is only next up is never sent.

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
- a value of the wrong type (ignored);
- in a stack: a `base` or `branch` that is not a plain branch name (Rex reports it), a branch two tasks share, that equals the base, or that nests with the base or another branch, a branch on a parent (ignored), or a task that depends on a later one.

Rex marks a file read-only, with the reason above its tasks, when its `version` is newer than 1 or unreadable (a quoted number such as `"1"` is read, with a warning), its `tasks` is not a list, or it still has git conflict markers: a tool that reads only v1 should not edit it. A missing `version` reads as 1. A file that is not valid JSON or that has a duplicate key is not shown: Rex keeps the last good copy on screen and names the line and column (`14:3`); for conflict markers it names the line of the first one.

## A new plan

Creating a plan in Rex's plan list writes every plan field except the optional `base`, empty, ready for an agent or a human to fill. It is the only plan file Rex writes:

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

## Teaching an agent

`rex ingest --install-skill` installs the `rex-plan` skill where every agent reads it: `~/.claude/skills/rex-plan/SKILL.md` (or `$CLAUDE_CONFIG_DIR/skills/`) for Claude Code and `~/.agents/skills/rex-plan/SKILL.md` for Codex; Cursor and OpenCode read both folders (and may list the skill twice: the copies are identical). With `$CLAUDE_CONFIG_DIR` set, a copy Rex wrote in `~/.claude/skills/rex-plan/` (Cursor and OpenCode still read it) is updated and uninstalled with the others; a `SKILL.md` there that Rex didn't write is left alone. With `--project` both copies go in the repo's `.claude/skills/` and `.agents/skills/`; `--uninstall-skill [--project]` takes Rex's copies back out. Each copy's generated marker carries the skill's version (`skill v3`): Rex calls a copy with an older version, or none, out of date, and `--install-skill` replaces it but never a newer Rex's copy. The skill teaches this format and its rules, how to work a plan (which plan and task, starting and finishing a task in the file, re-reading it every turn), how to read the `Rex note` that Claude Code's and Codex's prompt hooks add with the pinned task (docs/ux.md §7a), where a stacked task's PR goes, and a read-only review pass before each task's PR (one reviewer per job: design, the UI if it changed, breaking it, lint; each finding fixed or explained; then a final readiness check).
