# REX terminal UX spec

This is the target design for the REX CLI loop:

create → pin → watch → complete → send off → see drift → quit

It records each decision, the friction it removes, and which slice of work owns it. TUI changes cite the section they implement. `agents.md` is still the contributor contract. Where this spec changes an `agents.md` rule, the slice that ships it updates `agents.md` too (see [agents.md changes](#agentsmd-changes)).

Everything here keeps the product rules:
- watch-only;
- the plan JSON is the source of truth;
- yellow only, 40×40 poses;
- hooks only;
- Rex never sends anything to the agent.

---

## 1. Places

Rex has exactly **two places**. **Tab** switches between them and does nothing else.

| Place | Screens |
|---|---|
| **PLAN** | Library (Home, Find, Browse, Name prompt) · Outline · Plan detail · Task detail |
| **TAPE** | Live tape |

**Overlays** open on top of whichever place you are in: Completion, Send-off, Help, and Pin confirm. Esc closes them.

Each place remembers its screen, its selection, its scroll position, its draft, and any open overlay. Tab away and back, and everything is where you left it.
- Selection is kept by **identity**: plan path or task id. Row indexes are not used.
- A reload, resize or rebuild keeps the same plan or task selected. If that item disappears, the selection falls back to the pin, then to the nearest row.

**Events never move you.** Hook events, plan reloads and toasts change what is drawn. They never change the place, screen, selection or draft. Only keys do.

---

## 2. Input: nothing fires without ⏎

Letters always type into the composer. What the text means depends on the screen:

| Screen | Composer means | Prefix | Empty placeholder (dim) |
|---|---|---|---|
| Library Home | find a plan across `rex/plans/**` | ` / ` | `find or create a plan` |
| Browse | filter this folder | ` / ` | `type to filter` |
| Name prompt | plan or folder name | ` + ` | `plan name (a/b for a folder)` |
| Outline / detail | a verb | ` > ` | `s⏎ sendoff · d⏎ done · p⏎ pin · ?⏎` |
| Tape | a verb, or a local note | ` > ` | `note (not sent) · sendoff · ?` |
| Completion | the focused field | ` › ` | see §8 |

Keys that act without ⏎: the arrows, PgUp/PgDn, Home/End, Esc, Tab, Ctrl keys. Everything else waits for ⏎. Paste arrives as one token, and newlines and tabs inside it become spaces. **A paste never submits.** Scroll-wheel noise or a pasted blob therefore cannot write a plan file.

### Verbs

| Verb | Alias | Where | Does |
|---|---|---|---|
| `sendoff` | `handoff`, `s` | anywhere a plan is open | opens the Send-off overlay and copies the brief (§9) |
| `brief` | `b` | anywhere a plan is open | opens the planning brief ("fill or refine this plan") |
| `done` | `d` | anywhere a plan is open | opens Completion **for the pin** (§8) |
| `pin [id]` | `p` | outline / detail: the selection or the id; tape: needs an id | pins a task (§7) |
| `help` | `?` | anywhere | opens the Help overlay |
| `breakdance` | – | anywhere | plays the breakdance sheet once, then resumes the turn pose |

Rules:
- A leading `/` is accepted (`/sendoff`), the case of the letters does not matter, and `send off` works as two words.
- A verb is **never echoed** as a note. It leaves one muted result line instead, e.g. `sendoff · copied (pbcopy)`.
- While you type, the footer shows what ⏎ will do: `⏎ sendoff card-highlight` or `⏎ note (local, not sent)`. A typo is visible before you press ⏎.
- On the tape, any other text becomes a **note**:
  - It is labelled `── note · local ──` and drawn muted.
  - It exists only in Rex's memory. It is never written to the feed and never reaches the agent.
  - It does not start or end a turn.
- In the outline, any other text shows `not a verb · ?⏎ for help`.

### Editing

| Key | Effect |
|---|---|
| Backspace | Deletes one character. **Never navigates.** |
| Ctrl+U | Clears the line |
| Ctrl+W | Deletes the previous word |
| Ctrl+C | Clears the draft (or cancels the overlay) and arms quit; see §3 |

---

## 3. Keys

| Key | Library Home | Browse | Name prompt | Outline | Plan / Task detail | Tape | Overlays |
|---|---|---|---|---|---|---|---|
| ↑↓ PgUp/PgDn | move | move | – | move (2-row margin) | scroll | scroll 1 line / 1 page | move field / scroll |
| Home / End | first / last | first / last | – | first / last | top / bottom | oldest / live | – |
| Ctrl+A / Ctrl+E | – | – | – | – | – | oldest / live | – |
| ⏎, empty draft | open the selected card or recent | open the plan or enter the folder | "type a name first" | title row → Plan detail; task row → Task detail | – | – | confirm / save |
| ⏎, with draft | open the selected match, or create from `+ Create 'x'` | open the selected match | create, then go to its outline | run the verb | run the verb | run the verb, or add a note | save |
| → | – | enter the folder | – | Task detail | – | – | – |
| Esc / ← (empty draft) | clear find, else nothing | clear filter, else up one folder, else to Home | cancel, nothing written | to Library (cursor on **Current**) | back to Outline | clear draft | close, nothing written |
| Tab | to TAPE | to TAPE | to TAPE (draft kept) | to TAPE | to TAPE | to PLAN | the overlay stays open in its place |
| Ctrl+L | full repaint | full repaint | full repaint | full repaint | full repaint | full repaint | full repaint |
| Ctrl+C | clear + arm quit | clear + arm quit | clear + arm quit | clear + arm quit | clear + arm quit | clear + arm quit | cancel + arm quit |

The Esc ladder goes **up one level within the place**:
- Detail → Outline → Library Home.
- Browse → parent folder → Home.

At the top of the ladder, Esc does nothing. It never switches place and never quits.

**Quit.**
- The first Ctrl+C clears the draft or cancels an open overlay, and arms quit. The footer says `ctrl+c again to quit`.
- A second Ctrl+C within the window quits. The terminal is restored (cbreak off, alt screen left, cursor shown, bracketed paste off), and one plain line is printed:

```
rex: rex/plans/cuj/sendoff-demo.rex.json · pinned card-highlight
```

- Any other key, or the timeout, disarms quit and redraws.
- SIGTERM, SIGHUP and the Windows console close all take the same restore path.

---

## 4. Chrome

### Row 0: header

```
REX plan                       running · rex/plans/cuj/sendoff-demo.rex.json
```

- Left: `REX` plus the place word, `plan` or `tape`.
- Right: the turn-state word, then the open plan path relative to the repo root.
- The state word is colored, and `ask` / `error` use the error color.
- The same header appears on **every** screen. The turn state and the open plan are never hidden while you browse.
- When the path does not fit, it is shortened in this order:
  1. drop `rex/plans/`;
  2. drop `.rex.json`;
  3. clip with `…` from the left.

  So it reads `cuj/sendoff-demo` before `…ndoff-demo`.

### Row 1: status line

This row is blank today. It becomes a one-line status shared by both places. It shows the **first** of these that applies:

1. `plan JSON error 14:3 · showing last good`
2. `drift · hard: README.md is outside card-highlight files`
3. `2 active: card-highlight, load-outline · pinned card-highlight`
4. A toast, which expires after about 3 s:
   - `plan updated · plans-ui → done · +1 task`
   - `✓ card-highlight done · next up load-outline · p⏎ to pin`
5. `▸ card-highlight  Selected plan card is one block (active)`, or `(next up)` when the pin is computed rather than written.
6. `no pin · all tasks done` or `no pin · nothing unblocked`.

The line is empty when no plan is open.

### Last row: footer

The footer is built from **ranked segments**. When the width is short, whole segments are dropped, lowest rank first. Words are never cut. The first segment names the screen in words, so it still works under NO_COLOR.

| Screen | Footer (highest rank first) |
|---|---|
| Library Home | `plans · ⏎ open · type to find · tab tape · ctrl+c×2 quit` |
| Library Home (no plans) | `plans · ⏎ create · tab tape` |
| Browse | `open · ⏎ open · esc back · type to filter · tab tape` |
| Name prompt | `new plan · ⏎ create · esc cancel · tab tape` |
| Outline | `outline · s⏎ sendoff · d⏎ done · p⏎ pin · ⏎ detail · esc plans · tab tape` |
| Outline (empty plan) | `outline · b⏎ planning brief · tab tape` |
| Outline (read-only) | `read-only · esc plans · tab tape` |
| Plan / Task detail | `detail · ↑↓ scroll · p⏎ pin · esc outline` |
| Tape, empty draft | `tape · s⏎ sendoff · d⏎ done · tab plan · ?⏎ help` |
| Tape, typing | `⏎ note (local, not sent)` · `⏎ sendoff card-highlight` |
| Tape, scrolled | `12↑ · 3 new · end live · tab plan` |
| Completion | `done · ⏎ save · ↑↓ field · esc cancel` |
| Send-off | `copied (pbcopy) · c⏎ copy again · o⏎ print plain · esc close` |
| Pin confirm | `card-highlight is active · p⏎ again to move the pin · esc cancel` |
| Quit armed | `ctrl+c again to quit` |

### Composer

- The box is three rows: borders plus one input row.
- Below 16 terminal rows it shrinks to a single borderless row.
- The input row shows the end of a long draft, with a `…` on the left.
- The draft cap is visible: `256/256`.

---

## 5. Glyphs, color and NO_COLOR

| Glyph | Meaning |
|---|---|
| `▸` | pinned (active, or next up) |
| `○` | ready: pending, and all deps done |
| `·` | waiting on deps; the row also shows `after <id>` while a dep is unmet |
| `✓` | done (the row is muted as well) |
| `–` | skipped |
| `!` | blocked (status `blocked`) |
| `▌` | the selected row or card |

Rules:
- No glyph is an emoji, and the set avoids emoji-capable code points such as `▶` and `📌`.
- Meaning is never carried by color alone. Under `NO_COLOR` every state still reads from its glyph and its words.
- Color roles come from `mascot.UI`: `title`, `text`, `muted`, `you`, `pick`, `pickbg`, `folder`, `action`, `error`, `border`. No new palettes.
- Terminal.app has no 24-bit color. When `TERM_PROGRAM=Apple_Terminal` and `COLORTERM` is not `truecolor`/`24bit`, the same yellow palette is quantized to the 256-color cube.

---

## 6. Outline and detail

Opening or creating a plan lands on its **Outline**.

Mockup at 120×32 (78-col text column):

```
REX plan                       running · rex/plans/cuj/sendoff-demo.rex.json
▸ card-highlight  Selected plan card is one block (active)
── Send-off demo · active · 1/3 ──────────────────────────────── tasks 3/3 ──
  last ✓ plans-ui: Home cards, browse, duplicate warn

  ✓ plans-ui         Plans navigator
▌ ▸ card-highlight   Selected plan card is one block
▌     Bar, border, and fill cover the whole selected card
  · load-outline     Outline view                       after card-highlight

╭────────────────────────────────────────────────────────────────────────────╮
│ > s⏎ sendoff · d⏎ done · p⏎ pin · ?⏎                                       │
╰────────────────────────────────────────────────────────────────────────────╯
outline · s⏎ sendoff · d⏎ done · p⏎ pin · ⏎ detail · esc plans · tab tape
```

The same outline at 80×24 (38-col text column):

```
REX plan     running · cuj/sendoff-demo
▸ card-highlight (active)
── Send-off demo · active · 1/3 ──────
  last ✓ plans-ui: Home cards, brow…
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan ca…
▌     Bar, border, and fill cover t…
  · load-outline   after card-highl…
╭────────────────────────────────────╮
│ > s⏎ sendoff · d⏎ done · p⏎ pin    │
╰────────────────────────────────────╯
outline · s⏎ sendoff · d⏎ done
```

Layout:
- **The rule.** The title row reads `── <title> · <status> · <done>/<total> ──`, with `tasks <selected>/<total>` on the right once the list scrolls. ⏎ on the rule opens Plan detail (goal, scope in/out, plan problems).
- **The last line.** It shows the last finished task's `accomplished` text, the most useful "where we left off" sentence for a teammate.
- **Task order.**
  - Tasks are listed in **file order**, never topologically sorted. Moving deps around never reshuffles the list.
  - `parent` is shown as indentation, two levels at most. Deeper tasks get a `↳`.
  - A parent shows a rollup (`1/3`) instead of a status glyph.
  - **Only leaf tasks can be pinned.** Pinning a parent pins its first ready child.
- **The selected row** gets one muted blurb line: `does`, else `description`.
- **Scrolling.** The list keeps a two-row margin around the selection, and shows `more ↓` / `more ↑` when rows are hidden.
- **Task detail** (⏎ or → on a task) is a scrolling sub-screen:
  - title, status;
  - parent, `depends_on` (each with its state), and dependents;
  - does, needs, verify, files, scope in/out, description;
  - for a done task, the retrospective shown as intended (`does`) next to actual (`accomplished`, `modified`, `verified_how`, `completed_notes`).

### Plan states

| State | What you see | Writes |
|---|---|---|
| No tasks (a new stub) | `No tasks yet · your agent fills this file · b⏎ copies a planning brief` | – |
| Invalid JSON | row 1: `plan JSON error 14:3 · showing last good` | pin/done disabled, with a reason |
| File missing (branch switch) | `plan file missing · showing last good` | read-only |
| Conflict markers / `version` newer than Rex | `read-only: <reason>` | read-only |
| Two `active` tasks | first one pinned; row 1 warns | – |
| Bad reference | `after ?T9` in the error color; cycles are tagged `cycle` and cannot be pinned | – |
| All done / nothing unblocked | `no pin · all tasks done` / `no pin · nothing unblocked` | – |

Reload behaviour:
- **Invalid JSON is debounced.** It shows only after it persists for two polls, because agents save files in pieces.
- **External edits** (by the agent, a teammate's pull, or your editor) reload live. They show a toast, plus one `── plan ──` line on the tape such as `plans-ui → done (file) · +1 task`. Rex's own writes do not trigger the toast.

---

## 7. Pin

- **The effective pin.** It is the first `active` leaf task, else the first `pending` leaf whose `depends_on` are all `done` or `skipped`. With no tasks there is no pin; drift is then off and send-off says the plan is empty.
- **`p⏎` writes.** It writes `"status": "active"` on the target, and `"pending"` on the previous active task. Only those status values change in the file.
- **Confirmation.** Pinning a blocked task, or moving the pin while another task is `active`, asks again: `p⏎ again to move the pin`.
- **Reopening.** Pinning a `done` task reopens it. The status becomes `active`, and the retrospective is kept.
- **Pin start time.** It is recorded in `~/.rex/state.json`, outside the repo and never committed. It powers the completion evidence. The JSON gets no timestamps.
- **After `done`, nothing is auto-claimed.**
  - The cursor moves to the first newly ready task.
  - Row 1 says `next up <id> · p⏎ to pin`.
  - Drift and send-off use the effective pin in the meantime.

---

## 8. Completion overlay

`d⏎` / `done⏎` opens it **for the pin**. To complete another task, pin it first.

```
── done · card-highlight ──────────────────────────
  intended  Bar, border, and fill cover the whole card
  files     internal/tui/plans.go, internal/tui/live.go
            seen on this machine since 14:02 · may be partial
╭─────────────────────────────────────────────────╮
│ did ›  agent says: selection bar spans 3 rows   │
│ verified ›  go test ./internal/tui (passed)     │
╰─────────────────────────────────────────────────╯
done · ⏎ save · ↑↓ field · esc cancel
```

- **`did ›`** is required. It becomes `accomplished`.
  - If the agent's last reply ended with an `accomplished:` trailer, that text is shown as a dim **"agent says"** placeholder. ⏎ on the empty field accepts it.
  - Without a trailer, you type it.
- **`verified ›`** becomes `verified_how`.
  - It is suggested from what Rex saw: a command matching the task's `verify`, else a known test command.
  - It reads `(passed)` / `(failed)` only when a paired result was seen. Otherwise it reads `(result not seen)`.
- **`files`** becomes `modified`. The value is read-only here.
  - It lists completed edits seen in the feed since the pin started, inside this repo only.
  - It excludes `rex/plans/**` and never includes file bodies.
  - It is always labelled as partial.
- **Writes.**
  - Values already in the JSON win over suggestions, so an agent's own retrospective is never overwritten.
  - Saving writes `"status": "done"` plus only the fields that changed. The git diff stays inside that task.
- **Binding.** The overlay is bound to the task id. If that task changed on disk while it was open, it warns: `card-highlight changed on disk · ⏎ to save anyway`.
- **Esc** writes nothing.

---

## 9. Send-off and planning brief

`s⏎` / `sendoff⏎` / `handoff⏎` opens the **Send-off overlay** in the current place. The brief is generated from the open `.rex.json` at that moment. It never uses the tape or any transcript.

```
── sendoff · card-highlight ───────── copied (pbcopy) ──
  Paste into your agent chat. Rex never sends it.
  Send-off: Send-off demo
  File: rex/plans/cuj/sendoff-demo.rex.json
  Status: active · pinned task: card-highlight
  …
copied (pbcopy) · c⏎ copy again · o⏎ print plain · esc close
```

- **Content.** The brief follows the template in issue #9:
  - `Send-off: <title>`, then `File:` and `Status: … · pinned task: …`;
  - Goal;
  - Scope (In / Out);
  - Tasks (`[done]` / `[active]` / `[pending]`, with `(depends on: …)`);
  - Next (Does / Needs / Verify / Files);
  - For the next chat.

  Rex adds:
  - a short Done section (intended vs actual, modified, verified);
  - a request that the agent end its final reply with `accomplished:` / `verified:` lines;
  - the fingerprint `rex-sendoff: <path>#<task>`.
- **Size.** It aims for about 2 KB, with a 4 KB cap. Long fields are clipped with `(see plan file)`.
- **Copying is honest.**
  - Rex uses the native tool (`pbcopy`, `wl-copy`, `xclip`, `clip.exe`) and says `copied (<tool>)` only if it exited 0.
  - Over SSH, or with no tool available, it sends OSC 52 and says `sent (OSC 52) · may be blocked`.
  - `o⏎` leaves the alternate screen and prints the plain brief to the normal screen. Mouse selection is then clean, and the text stays in scrollback. ⏎ returns.
- **Saved copy.** The brief is also written to `~/.rex/sendoff.md`, outside the repo, and regenerated every time. It is not a second source of truth.
- **On the tape.** Rex adds a one-line record: `── sendoff · card-highlight · copied (pbcopy) ──`. When the brief comes back as a user prompt (it carries the fingerprint), the tape collapses it to `── sendoff ── card-highlight pasted (24 lines)`. That confirms the new chat started from the pin.
- **An empty plan** (no tasks) refuses: `nothing to send · this plan has no tasks · b⏎ copies a planning brief`.

`b⏎` / `brief⏎` copies the **planning brief**:
- the plan path;
- the v1 shape in a few lines;
- the field rules (ids are short stable slugs; file order is do order; `parent` groups tasks; `depends_on` lists hard prerequisites only; one task should fit one PR);
- `fill or refine <path>`.

The outline reloads live while the agent writes the file.

---

## 10. Tape

- **Blocks.** `thinking`, `run`, `ask`, `reply`, `error`, `note · local`, `plan`, `drift`, `sendoff`.
  - File-look commands and result echoes stay hidden.
  - Each reply is its own block.
- **Prompt boxes.** User prompt boxes are right-aligned with left-aligned text. They are capped at 6 rows, followed by `(+N lines)`.
- **Text cleanup.** Agent text is cleaned before layout:
  - Control characters and escape/OSC sequences are removed, so agent output cannot recolor the screen, retitle the window or write the clipboard.
  - Tabs are expanded.
- **Scope.** Paths are shown relative to the repo. Events from other repos' agents (same machine, same feed) are hidden, and the count appears under Help. Events with no known working directory are shown.
- **Starting mid-turn.** If the newest feed event is under 2 minutes old, the turn state is restored, so the dino is already typing. The last ~20 events from this repo are drawn under a muted `── before rex started ──` rule.
- **Feed health** replaces the bare "Ready. Watching…":

```
── feed ── waiting for agent events
  feed     ~/.rex/session.jsonl · last event 3h ago (cursor)
  cursor   wired
  claude   not wired · rex ingest --install claude
  4 events from other folders hidden
```

- **Submitting.** Sending a note while scrolled up jumps back to live.

---

## 11. Library

- **Cards, in order:**
  1. **Continue `<plan>`**: the last plan opened in this repo, from `~/.rex/state.json`.
  2. **Create a plan**.
  3. **Open a plan**.
  4. **Open latest**: the newest plan by modification time.

  Recents follow. The cursor starts on Continue, else Open latest, else Create.
- **First run (no plans).** Only Create shows, plus `plans are JSON in rex/plans/ · commit them`.
- **Card hints** show progress, e.g. `2/5 · ▸ card-highlight`. The currently open plan is marked in recents and in Browse.
- **Find (typing on Home)** matches across `rex/plans/**`.
  - The first match is selected.
  - With no match, the selected row becomes `+ Create 'x'`, and ⏎ creates it with that name.
- **Browse:**
  - The cursor starts on the first folder or plan, and a filter selects its first match.
  - `[ + New folder ]` / `[ + New plan ]` sit **below** the listing.
  - Entering or leaving a folder clears the filter.
  - ⏎ on a non-plan file says `not a .rex.json`.
  - With nothing matching, it shows `no match for 'x' in rex/plans/sub`.
- **Name prompt:**
  - `a/b` creates the plan inside folder `a`, and the target path is shown before ⏎.
  - Duplicates warn and never overwrite.
  - A new plan is a valid v1 stub, not `{}`.
- **Errors** clear on the next key.
- **Rex never deletes or creates files just by starting.**
  - `rex/plans/` is created on the first Create.
  - A file named `rex` at the repo root shows `rex/ is a file here · plans off` instead of being removed.
  - Outside a git repo, Home says `not in a git repo · plans go to <cwd>/rex/plans`.

---

## 12. Layout tiers

| Terminal | Layout |
|---|---|
| ≥ 66 cols | mascot (40) + gutter (2) + text column (≥ 24) |
| < 66 cols | **text only**: the mascot is hidden and the text column uses the full width |
| text column < 24 cols | `REX · widen to 24 cols`; keys other than Ctrl+C are ignored |
| < 16 rows | one-row borderless composer |
| < 7 rows | printable keys are ignored, so nothing is typed blind |
| `TERM=dumb`, pipes, CI | the static card (`rex start --static`) |

Notes:
- **No full clears.** Rex never clears the whole screen on a normal frame. Changed rows are repainted and erased to end of line, inside synchronized-output markers. The cursor is hidden while painting.
- **Repaints.** Resize, `SIGCONT` and Ctrl+L force a full repaint.
- **Widths.**
  - Text is clipped by **cell width**: East Asian wide characters and emoji count 2, combining marks 0.
  - Ambiguous-width box glyphs count 1. "Ambiguous characters are wide" terminal settings are not supported.

---

## 13. Help overlay (`?⏎`)

The overlay lists:
- the keys and verbs (§2, §3);
- the glyph legend (§5);
- what Rex never does: run tools, approve tools, send anything to the agent, or read transcripts;
- the feed path and feed health (§10);
- the hidden-event count.

Esc closes it.

---

## 14. Keystroke budget

The full-loop tests check these counts.

| Step | Keys |
|---|---|
| Open the plan you had last time | ⏎ |
| Open a plan by name | `name` ⏎ |
| Create a plan | ⏎ `name` ⏎ (lands on its outline) |
| See the pin | 0 (row 1) |
| Watch the tape | Tab |
| Pin a task | ↑↓ `p` ⏎ |
| Mark the pin done | `d` ⏎ `text` ⏎ (or `d` ⏎ ⏎ to accept the agent's line) |
| Send off | `s` ⏎ |
| Back to the plan | Tab |
| Quit | Ctrl+C Ctrl+C |

---

## 15. Transitions

| State | Entered via | Left via |
|---|---|---|
| Library Home | start; Esc from Browse root or from Outline | ⏎ card/recent/match · typing (find) · Tab |
| Browse | ⏎ Open a plan | ⏎ plan → Outline · Esc/← · Tab |
| Name prompt | Create card · `+ Create 'x'` · `+` rows | ⏎ → Outline (plan) or into the new folder · Esc (nothing written) · Tab (draft kept) |
| Outline | open or create a plan · Esc from detail · Tab back | ⏎/→ detail · Esc → Library · verbs · Tab |
| Plan detail | ⏎ on the outline rule | Esc |
| Task detail | ⏎/→ on a task | Esc |
| Pin confirm | `p⏎` on a blocked task or over another active task | `p⏎` again writes · any other key cancels |
| Completion | `d⏎`/`done⏎` with a pin | ⏎ save (toast) · Esc · Ctrl+C |
| Send-off | `s⏎`/`sendoff⏎`/`handoff⏎` | Esc · `o⏎` (plain print, ⏎ returns) |
| Help | `?⏎`/`help⏎` | Esc |
| Tape | Tab | Tab · verbs |
| Quit armed | Ctrl+C | Ctrl+C quits · any key / timeout disarms |

---

## 16. Friction register

This is what the 2026-10 audit found, from four lenses: first run, daily loop, teammate handoff, and terminal robustness. Each item lists its symptom, the fix, and the slice that owns it. Code locations are as of `dev` @ `4318b56`.

| ID | Sev | Friction (symptom) | Fix | Slice |
|---|---|---|---|---|
| F1 | critical | Start deletes a file named `rex`, `RemoveAll`s `rex-plans/` after a name clash, and creates `rex/plans/` in `$HOME` (plans.go:101-122) | Never delete; create lazily; show notices instead (§11) | `fix/plans_dir_safety` |
| F2 | high | Quitting can leave the TTY broken: Windows Ctrl+C kills the process; no SIGTERM/SIGHUP restore | Windows raw input mode; signals take the restore path (§3) | `fix/key_input` |
| F3 | high | Esc does nothing on Windows (0x1B is not mapped) | Map it; join surrogate pairs | `fix/key_input` |
| F4 | high | Fast arrows or the wheel drop keys and type stray `A`/`B`; paste submits; `é` becomes `��`; Ctrl+S freezes output | Buffered tokenizer, bracketed paste, IXON/IEXTEN off | `fix/key_input` |
| F5 | high | Agent text is painted raw: escape/OSC sequences can repaint the screen or set the clipboard | `CleanText` at the render boundary (§10) | `fix/text_sanitize` |
| F6 | high | Below 66 cols the text column vanishes, but keys still act | Layout tiers (§12) | `feature/ux_chrome` |
| F7 | high | Local composer text renders as `── reply ──`; the pane is called "chat" | Local `note` kind; "tape"; no echoed verbs (§2) | `feature/ux_input` |
| F8 | high | Browse: typing a filter then ⏎ opens "New folder"; the filter leaks into subfolders | Selection follows the filter; `+` rows below; clear on folder change (§11) | `fix/library_navigation` |
| F9 | high | Creating or opening a plan shows nothing of the plan; files are `{}` | v1 stub; land on the outline (§6) | `feature/plan_schema`, `feature/plan_outline` |
| F10 | high | An empty plan dead-ends the loop | Empty state + planning brief (§9) | `feature/sendoff` |
| F11 | high | The global feed mixes repos; paths are absolute or cwd-relative | cwd/session meta; repo-relative paths; repo scope (§10) | `feature/event_scope` |
| F12 | high | No data for `modified`/`verified_how` (results never reach the log) | Evidence from raw events + feed since pin start (§8) | `feature/task_completion` |
| F13 | high | The empty tape can't say whether any hook is wired | Feed health (§10) | `feature/harness_hooks` |
| F14 | med | Turn state is wrong: Claude never idles; a tool failure idles mid-turn; restart shows idle | Map Stop; tool errors keep the turn running; warm start | `feature/harness_hooks`, `feature/event_scope` |
| F15 | med | Outline hotkeys would collide with the composer and with paste | ⏎-only verbs (§2) | `feature/ux_input` |
| F16 | med | Esc and Backspace navigate inconsistently (Esc on Home jumps to the tape) | Esc ladder; Backspace only deletes (§3) | `feature/ux_input` |
| F17 | med | The plans header drops turn state and path; the open plan is invisible in the library | Header on every screen; Current card (§4, §11) | `feature/ux_chrome`, `feature/plan_outline` |
| F18 | med | Home: inert composer, dead cards, sticky errors, Open latest takes ↓↓⏎ | Find on Home; hide dead cards; clear errors; default cursor (§11) | `fix/library_navigation` |
| F19 | med | Lists have no scroll position; selection is a row index | Scroll offset + margin; selection by identity (§1, §6) | `fix/library_navigation` |
| F20 | med | Footers are cut mid-word at 80 cols | Ranked segments (§4) | `feature/ux_chrome` |
| F21 | med | The existing write pattern reorders keys and isn't atomic; agents edit the same file | Byte-span edits, hash re-check, atomic writes | `feature/plan_store` |
| F22 | med | Live-reload edge states are undefined | Debounce, last good, read-only states (§6) | `feature/plan_outline` |
| F23 | med | Pin semantics in a shared file (claiming, blocked, parents) | §7 | `feature/pin_task` |
| F24 | med | Copy can claim success falsely; pasted briefs flood the tape | Honest copy; plain print; fingerprint collapse; 6-row cap (§9, §10) | `feature/sendoff`, `feature/clipboard` |
| F25 | med | Drift would be noisy and shown in too many places | Row 1 + one block per pin; plain reasons | `feature/prompt_drift` |
| F26 | med | A dense outline overflows 38×18 | §6 layout | `feature/plan_outline` |
| F27 | med | Every frame clears the screen: flicker, wasted CPU | Diff renderer + caches (§12) | `fix/render_flicker` |
| F28 | med | The tail drops replies that grow (the last line is rewritten in place) | Append-only feed | `fix/ingest_fail_open` |
| F29 | med | 24-bit color only; Terminal.app on macOS ≤ 15 has none | 256-color quantization of the same palette (§5) | `fix/render_flicker` |
| F30 | med | Tab drops a half-typed name; `a/b` silently becomes `b` | Drafts per place; subfolder create (§11) | `feature/ux_input`, `fix/library_navigation` |
| F31 | low | No Ctrl+U/W; silent 256 cap; stale "press again" footer | §2, §3 | `feature/ux_input` |
| F32 | low | Nothing is discoverable (breakdance only in the README) | Help overlay, placeholders (§13) | `feature/ux_input` |
| F33 | low | Width counted in runes: CJK titles break borders | Cell-width clipping (§12) | `fix/text_sanitize` |
| F34 | low | README says teammates watch the same feed (feeds are per machine) | Reword | `feature/plan_schema` |
| F35 | low | No trace after quitting | Exit line (§3) | `feature/ux_chrome` |

Found in the same audit, outside the UI:

| ID | Sev | Problem | Fix | Slice |
|---|---|---|---|---|
| H1 | critical | `rex ingest` exits 2 on a bad flag or source; Claude Code, Cursor and Codex treat exit 2 as **block** | Ingest always exits 0 with empty stdout; stdin and lock deadlines | `fix/ingest_fail_open` |
| H2 | high | The Cursor installer overwrites `hooks.json` it can't parse; writes non-atomically; quotes with Go syntax; registers shell commands twice | Safe merge, `.bak`, atomic write, shell quoting | `fix/cursor_installer` |
| H3 | med | Every hook call reads the whole feed file | Seek-based tail read | `fix/ingest_fail_open` |

---

## agents.md changes

These `agents.md` and README lines change when the slice that implements them ships:

| Slice | Line today → after |
|---|---|
| `fix/ingest_fail_open` | Watch feed: `AppendEvent` dedupes → append-only; ingest always exits 0 with empty stdout |
| `fix/plans_dir_safety` | "Ensures rex/plans (replaces a colliding rex file…)" → created on first Create; never deletes |
| `fix/key_input` | "Unix live mode is cbreak (ECHO/ICANON/ISIG off) and must keep OPOST" → also IXON and IEXTEN off |
| `fix/text_sanitize` | "Clip ANSI by cells (runes)" → clip by cell width; agent text is cleaned first |
| `fix/render_flicker` | "Yellow from assets/idle.png is the only look" → plus the 256-color quantization of that palette for Terminal.app |
| `feature/ux_chrome` | "Left ~40 columns: mascot" → layout tiers; header on every screen |
| `feature/ux_input` | Kinds: + local-only `note`; "Tab switches to chat" → two places; "chat" → "tape" |
| `feature/event_scope` | "`NewEventTail(path, true)` paints new lines" → plus warm start and repo scope |
| `feature/harness_hooks` | "Only Cursor hooks are installed" → which harnesses are wired, and that the others are opt-in |
| `feature/plan_schema` | "Build plan JSON schema … unless asked" → schema is v1 in `docs/plan-format.md`; README "new plans start as `{}`" |
| `feature/plan_outline` | "Opening a plan is a no-op load" → opening parses the plan read-only and shows the outline |
| `feature/sendoff`, `feature/prompt_drift` | Send-off is generated from the JSON; drift is display-only |
