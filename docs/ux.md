# REX terminal UX spec

This is the target design for the REX CLI loop:

the agent writes a plan (Rex opens it) → the agent pins a task → watch → see drift → the agent marks it done → quit

The agent drives the plan; Rex only shows it. Rex has no pin, done, send-off or brief actions: you ask your agent, it writes `active` and `done` (and the completion fields) into the plan JSON, as the `rex-plan` skill teaches, and Rex reloads and shows it. The only thing you type in Rex is a find, filter or name in the plan list.

It records each decision, the friction it removes, and which slice of work owns it. TUI changes cite the section they implement. `AGENTS.md` is still the contributor contract. Where this spec changes an `AGENTS.md` rule, the change that ships it updates `AGENTS.md` too.

Everything here keeps the product rules:
- watch-only;
- the plan JSON is the source of truth, and Rex never writes it (except a new empty plan you create in the plan list);
- yellow only, 40×40 poses;
- hooks only;
- Rex never sends anything to the agent.

---

## 1. Places

Rex has exactly **two places**. **Tab** switches between them and does nothing else.

| Place | Screens |
|---|---|
| **PLAN** | Wait (no plan yet) · plan list (Home, Find, Browse, Name prompt) · Outline · Plan detail (About) · Task detail |
| **AGENT** | what the agent is doing, live (§10) |

**Both places are always on row 0**, in the same order, the one on screen as a pill: `[PLAN] AGENT` or `PLAN [AGENT]` (§4). The cell between the two names is the **cue**: `•` when the other place has something new, `!` when it needs you. Showing a place clears its cue, nothing builds up for the place on screen, and a cue never moves you, your selection or your scroll.

**Every PLAN screen, and Help, has a page title** on row 2: where you are as a breadcrumb (`PLANS › Send-off demo › card-highlight`). The breadcrumb is the Esc ladder: Esc goes to the crumb on its left, and the footer's Esc word names it (§3, §4). AGENT has one from 12 rows.

**Overlays** open on top of whichever place you are in. There is one: Help. Esc closes it, and Tab leaves it open in its place. (Completion, Send-off and Pin confirm are gone, with the actions they served: the agent writes the plan, §7–§9.)

Each place remembers its screen, its selection, its scroll position and any open overlay; PLAN also keeps the plan list's draft, the only one there is (§2). Tab away and back, and everything is where you left it, unless an attach opened another plan in PLAN meanwhile (below, §11).
- Selection is kept by **identity**: plan path or task id. Row indexes are not used.
- A reload, resize or rebuild keeps the same plan or task selected. If that item disappears, the selection falls back to the pin, then to the nearest row. One more exception: when a plan with no tasks (or one that didn't parse) gains its first tasks, the selection goes to the pin, as opening the plan does.

**Events never move you, with one exception.** Hook events, plan reloads, toasts and cues change what is drawn. They never change the place, selection or draft. Only keys do, apart from the two selection fallbacks above and **attach** (§11): while the session is unlatched (*following*), a completed write of a plan file in this repo's `rex/plans/` opens that plan's outline in PLAN, and so does a plan file the cold-start scan finds while no plan is open. Even then the place never changes (on AGENT you stay on AGENT, and PLAN's cue lights), and text in the plan list's composer or an open Name prompt holds the attach until it clears; row 1 says it is waiting. A latched session is never moved: the new plan is only offered on row 1 (§4), and taking it waits for the same things.

---

## 2. Input: the composer is only in the plan list

Rex has no actions of its own on a plan, so most screens have nothing to type. The **composer** exists only in the plan list, where text finds, filters or names a plan:

| Screen | Composer means | Prefix | Empty placeholder (dim) |
|---|---|---|---|
| plan list Home | find a plan across `rex/plans/**` | ` / ` | `find or create a plan` |
| Browse | filter this folder | ` / ` | `type to filter` |
| Name prompt | plan or folder name | ` + ` | `plan name (a/b for a folder)` / `folder name` |

The Wait screen, the Outline, Plan / Task detail, AGENT and Help have **no composer box**; their body takes those rows. There, keys act at once (§3), and every other printable key and every paste does nothing: no hidden draft is built, so nothing typed there can ever act later.

**In the plan list:**
- Text is taken literally, so typing `done` and pressing enter there finds or names "done", and enter opens the selected match (or creates from `+ Create 'x'`).
- A leading `/` forces a verb: `/help` and `/breakdance` (plays the breakdance sheet once, then resumes the turn pose), each followed by enter. Any other `/word` says `'/word' is not a verb · try /help`. The footer previews it before enter: `enter help`, `enter not a verb`.
- A paste arrives as one token, and newlines and tabs inside it become spaces. **A paste never submits.**

**`?` is a single key everywhere.** On a screen without a composer it opens Help at once, no enter. In the plan list, a `?` typed into an **empty** composer opens Help at once too (nobody finds a plan named "?"); a `?` after other text is just text. Inside Help it does nothing. So every footer can say `? help`.

Rex's own lines on AGENT (`rex › plan · …`, `rex › drift · …`, `── before rex started ──`, §10) exist only in Rex's memory. They are never written to the feed, never reach the agent, and never start or end a turn.

### Editing (plan list only)

| Key | Effect |
|---|---|
| Backspace | Deletes one character. **Never navigates.** |
| Ctrl+U | Clears the line |
| Ctrl+W | Deletes the previous word |
| Ctrl+C | Clears the draft (or closes Help) and arms quit; see §3 |

---

## 3. Keys

Keys are written as words everywhere Rex shows them: `enter`, `esc`, `tab`, `?`, `↑↓`, `home`, `end`, `ctrl+c`.

| Key | plan list Home | Browse | Name prompt | Outline | Plan / Task detail | AGENT | Help |
|---|---|---|---|---|---|---|---|
| ↑↓ PgUp/PgDn | move | move | – | move (2-row margin) | scroll | scroll 1 line / 1 page | scroll |
| Home / End | first / last | first / last | – | first / last (Home is the title row) | top / bottom | oldest / live | – |
| Ctrl+A / Ctrl+E | as Home / End | as Home / End | – | as Home / End | as Home / End | oldest / live | – |
| enter | open the selected card, recent or match, or create from `+ Create 'x'`; a `/word` runs it | open the plan or enter the folder | create, then open its outline (plan) or enter it (folder); empty: "type a name first" | title row (the page title) → Plan detail; task row → Task detail; with a new-plan offer on row 1, switch to it | with an offer, switch to it | with an offer, switch PLAN to it (you stay on AGENT) | nothing |
| → | – | enter the folder | – | Task detail | – | – | – |
| Esc | clear the find text, else back to Wait while no plan is open, else nothing | clear the filter, else up one folder, else to Home | clear the name, else cancel (nothing written) | to the plan list (cursor on **Continue**) | back to Outline | nothing | close |
| ← | (empty draft) back to Wait while no plan is open | (empty draft) up one folder, else to Home | – | to the plan list | back to Outline | – | – |
| `?` | Help, when the composer is empty | Help, when the filter is empty | Help, when the name is empty | Help | Help | Help | nothing |
| letters, paste | type | type | type | nothing | nothing | nothing | nothing |
| Tab | to AGENT | to AGENT | to AGENT (name kept) | to AGENT | to AGENT | to PLAN | Help stays open in its place |
| Ctrl+L | full repaint | full repaint | full repaint | full repaint | full repaint | full repaint | full repaint |
| Ctrl+C | clear + arm quit | clear + arm quit | clear + arm quit | arm quit | arm quit | arm quit | close + arm quit |

The Wait screen (§11) has no composer either: ↑↓, PgUp/PgDn and Home/End (or Ctrl+A/Ctrl+E) scroll its copy when it runs past a short screen; Esc or ← goes to the plan list's Home; `?` opens Help; enter and letters do nothing. AGENT's empty tab (feed health, §10) is a page too and scrolls the same way: there Home/End (and Ctrl+A/Ctrl+E) are its top and bottom.

enter switches to a new-plan offer only while row 1 shows it (the plan file's error, a toast or hard drift cover it, §4) and opening it would lose nothing: not while the plan list's composer has text or a Name prompt is open (§11), nor inside Help. Otherwise row 1 drops `· enter switch` (or doesn't show the offer) and enter does what it does without an offer.

In the plan list, Esc first clears a non-empty draft. Otherwise it goes **up one level within the place**, to the crumb on the left of the page title (§4), and the footer names where: `esc plans`, `esc outline`, `esc up`, `esc clear`, `esc cancel`, `esc close`, and `esc wait` on the plan list while no plan is open.
- Detail → Outline → plan list Home (`PLANS › Send-off demo › about`).
- Wait → plan list Home → Wait, while no plan is open: Esc (or ←) takes you back and forth, so the Wait screen is never lost, and the plan list's footer says `esc wait`. With a plan open, Home is the top.
- Browse → parent folder → Home (`PLANS › rex/plans/ › cuj/`).

At the top of the ladder, Esc does nothing. It never switches place and never quits.

**Quit.**
- The first Ctrl+C closes Help, clears the plan list's draft when you are in PLAN, and arms quit. The footer says `ctrl+c again to quit`.
- A second Ctrl+C within the window quits. The terminal is restored (cbreak off, alt screen left, cursor shown, bracketed paste off), and one plain line is printed:

```
rex: rex/plans/cuj/sendoff-demo.rex.json · pinned card-highlight
```

(`· next up <id>` when no task is active.)

- Any other key, or the timeout, disarms quit and redraws.
- SIGTERM, SIGHUP and the Windows console close all take the same restore path.

---

## 4. Chrome

Every screen has the same frame:

| Row | Content | Dropped? |
|---|---|---|
| 0 | Tab bar: `REX`, the two tabs and their cue, the turn word, the follow word, the open plan's path. | Never. Parts shorten by rank. |
| 1 | Status: the pin, a toast, drift, an error or an offer, with the plan's progress on the right. | Never. Empty when no plan is open. |
| 2 | Page title: a breadcrumb and its meta on a slate bar. | Never on PLAN screens or Help. AGENT has one only from 12 rows. |
| 3… | Body. | Each screen has its own. |
| last−3…last−1 | Composer, plan list only (below). | – |
| last | Footer: keys. | Never. |

### Row 0: tab bar

```
REX [PLAN]•AGENT thinking        following rex/plans/cuj/sendoff-demo.rex.json
REX PLAN•[AGENT] running           latched rex/plans/cuj/sendoff-demo.rex.json
REX [PLAN]!AGENT ask             following rex/plans/cuj/sendoff-demo.rex.json
REX [PLAN] AGENT idle
```

- `REX`, in bold, shows while the text column is at least 42 cells: at 120 columns, and on text-only terminals of 42–65 columns. At 80×24 the mascot is the brand.
- **The tabs.** Both are always there, PLAN first. The one on screen is a pill, `[PLAN]` or `[AGENT]`: in color a white fill with the word in bold, the brackets painted in the fill so they vanish; under NO_COLOR the brackets say which. The other is muted.
- **The cue** is the one cell between the names: a space, `•` (something new there) or `!` (something there needs you); `!` outranks `•`.
  - On PLAN, AGENT's cue: `•` for a reply; `!` for a question, or a turn's own error (not a failed tool call). Thoughts, commands, failed tool calls, results, file looks, other repos' events and Rex's own lines light nothing.
  - On AGENT, PLAN's cue: `•` when a reload brings a change, an attach opens (or holds) a plan, or row 1 offers one; `!` when the open plan's file is gone or doesn't parse.
  - Showing a place clears its cue. It lives in memory only.
- **The turn word** follows the tabs: `idle` (muted), `thinking` / `running` (yellow, live agent work), `ask` / `error` (red, bold). The tabs and cue are always 12 cells, so the turn word never moves.
- **The right side**, while a plan is open: `following` (muted) or `latched` (§11), then the open plan's path relative to the repo root. With no plan open it is empty. The path is shown whole when it fits, else shortened in this order:
  1. drop `rex/plans/`;
  2. drop `.rex.json`;
  3. `…/` and its last part (`…/sendoff-demo`);
  4. `…` and its tail, down to 5 cells (`…demo`); under 5 cells it goes.
- `following` / `latched` outrank the path's tail: the word stays while it fits beside 5 cells of path (fewer for a shorter name), judged against the longest turn word (`thinking`) and the longer word (`following`), so it doesn't come and go with the turn and both words come and go together. A dropped word never gives `.rex.json` its room back. From 38 cells (80×24 with the mascot) it always shows:

```
[PLAN]•AGENT thinking  following …demo
[PLAN] AGENT idle  following …off-demo
PLAN•[AGENT] running   latched scratch
[PLAN] AGENT idle
```

### Row 1: status line

Row 1 is a one-line status shared by both places. It shows the **first** of these that applies:

1. `plan JSON error 14:3 · showing last good`
2. A toast, which expires after about 3 s:
   - `plan updated · plans-ui → done · +1 task`, when the file changed (the agent pinned a task, or a teammate's pull); the plan's own status reads `plan → active` / `plan → done`
   - `✓ card-highlight done · next up load-outline`, when the file marks the pin done (the agent finished it; `· now load-outline` when the same save started that task; `· the plan is done` or `· nothing else is unblocked` when there is no next up). The agent's follow-up saves that move no task (its retrospective, the plan's own status) leave it up until it expires (§8).
   - `following cuj/next`, when Rex opened a plan the agent wrote
   - `rex-plan skill is out of date · rex ingest --install-skill` (or `… --project`), when start opens a plan and an older Rex's skill is installed (§11); narrow, `rex-plan skill out of date · ? help`
   - a plan list notice (outside git, an old `rex-plans/`) when start opens a plan, unless the skill line above took the toast
3. `drift · hard: README.md · outside card-highlight files` (§9a)
4. A new plan (§11):
   - `new plan cuj/next · enter switch`: a latched session's agent wrote another plan. It stays until enter takes it or you open a plan (enter on Continue settles it too); the lines above cover it while they last, and enter switches only while row 1 shows it. It reads just `new plan cuj/next` where enter doesn't take it: in the plan list, where enter opens the selected row, and while taking it would lose the plan list's draft or a Name prompt (§3, §11).
   - `new plan cuj/next · opens when the line is clear`: a following session holds the agent's write until the plan list's draft clears. While a Name prompt is open, even an empty one, it reads `new plan cuj/next · opens when the prompt closes`: only closing the prompt lets it through.
   - Narrow, the name is clipped from the left (`new plan …demo · enter switch`, `new plan …demo · waiting`), then the tail goes (`new plan cuj/next`), and the name goes only when fewer than 5 cells are left for it.
5. `drift · soft: internal/tui/live_view.go · next to live.go`
6. `2 active: card-highlight, load-outline · pinned card-highlight`
7. `▸ card-highlight  Selected plan card is one block`, the task the agent made active, or `▸ load-outline  Outline view (next up)` when no task is active and the pin is computed (§7). The title clips with `…`; with fewer than 8 cells left for it, the line is just `▸ card-highlight` (or `▸ load-outline (next up)`).
8. `no pin · all tasks done` or `no pin · nothing unblocked`.

While a plan is open, whatever row 1 says has the plan's **progress** on the right in muted text, `1/3 done` from 60 cells, else `1/3`, two cells from it, from the same count as the outline's leaves. Progress never costs the line its words: it shows only where the line on its left reads exactly as it does across the whole width, or where only the pin's title clips for it (with `…`, down to 8 title cells, then `▸ id` alone, as the pin line always narrows), and keeps 12 cells there. A line that fits the row whole takes `1/3` when `1/3 done` won't fit beside it, and no progress when neither will; any other line fitted to the row (the done toast that keeps the next id, `✓ …ghlight done · next up load-outline` at 38 cells, a held plan's `· waiting`) keeps the row unless that same fitted form fits beside the progress. With no plan open, only a toast or a held new plan shows, with no progress; otherwise the line is empty.

The pin's `▸ id` is bold yellow, the pin's color; `(next up)` is muted; a key on row 1 (`enter`, `?`) is drawn as a key, in bold.

```
▸ card-highlight  Selected plan card is one block                     1/3 done
▸ load-outline  Outline view (next up)                                1/3 done
new plan cuj/next · enter switch                                      1/3 done
▸ card-highlight  Selected plan…   1/3
▸ load-outline (next up)           1/3
new plan cuj/next · enter switch   1/3
drift · hard: README.md            1/3
```

### Row 2: page title

Row 2 is a full-width slate bar: a breadcrumb from column 2, the root crumb in capitals and crumbs joined by ` › `, the current crumb bold, and the meta on the right, ending one cell from the edge. Under NO_COLOR it is the same text with no bar. **The breadcrumb is the Esc ladder**: Esc goes to the crumb on the left, and the footer's Esc word names it.

| Screen | Crumbs | Meta | Esc word |
|---|---|---|---|
| plan list | `PLANS` | `4 plans` | `esc wait` while no plan is open, else – |
| Find | `PLANS › find` | `1 match` / `N matches` | `esc clear` |
| Browse | `PLANS › rex/plans/` (deeper: `› cuj/`) | plans in that folder and below; with a filter, those under the rows it leaves | `esc plans` at the top, else `esc up` |
| Name prompt | `PLANS › new plan` / `new folder` (from Browse: `PLANS › rex/plans/ › cuj/ › new plan`) | – | `esc cancel` (`esc clear` with a name) |
| Wait | `PLANS › waiting for a plan` | – | `esc plans` |
| Outline | `PLANS › <plan title>` | the plan's status (`active`); `· tasks 3/8` once the list scrolls | `esc plans` |
| Outline, unreadable | `PLANS › <file stem>` | `can't read` (red) | `esc plans` |
| Plan detail | `PLANS › <title> › about` | – | `esc outline` |
| Task detail | `PLANS › <title> › <task id>` | – | `esc outline` |
| AGENT (from 12 rows) | `AGENT` | `live` · `12↑` · `12↑ · 5 new` · `waiting for events` | – |
| Help | the crumbs of the page under it, then `HELP` | – | `esc close` |

When the row is too narrow: the meta drops; then middle crumbs clip with `…`, down to 6 cells each; then the root becomes `…`; then middle crumbs fold into it while the current crumb would get fewer than 6 cells; and only then is the current crumb cut. Crumbs are never re-cased.

```
  PLANS › Send-off demo        active
  PLANS › Send-off d… › card-highlight
  AGENT                   12↑ · 5 new
  … › Send-… › card-hig…
```

**The title row.** On the outline the page title is the selectable title row, above the tasks: ↑ from the first task, or Home, selects it (`▌` and the selection's fill), and enter opens Plan detail.

### Last row: footer

The footer lists **keys**, never the screen's name: the tab pill and the page title say where you are. Rex has no verbs to list; in the plan list the composer placeholder (§2) says what you can type.

- Each segment is a key in bold and what it does, muted: `esc plans`. Keys are words: `enter`, `esc`, `tab`, `?`, `↑↓`, `home`, `end`, `ctrl+c`.
- Segments are joined by two spaces. Rank is display order, navigation first: the footer keeps the longest prefix that fits, and the last one kept may shorten (`? help` → `?`). Words are never cut.
- The 38-cell column below is each screen's must-haves at 80×24, and a test pins every row. Help (`?`) always lists every key.
- `? help` (or `?`) is a must-have wherever `?` opens Help, with two exceptions: Help itself, where `?` does nothing (§2), and Plan / Task detail while row 1 offers a plan, where `enter switch  esc outline  tab agent` takes 36 cells and `  ?` would make 39. While you type in the plan list (Find, a name, a `/word`) a `?` is text, so those footers don't offer it.
- Under 7 rows the plan list's hidden composer takes no enter (§12): its footer keeps only what works.

| Screen | 38 cells (80×24) | Every segment, in rank order |
|---|---|---|
| plan list | `enter open  tab agent  ? help  ↑↓ move` | `enter open  tab agent  ? help  ↑↓ move  type to find` |
| plan list, on Create | `enter create  tab agent  ? help` | `enter create  tab agent  ? help  ↑↓ move  type to find` |
| plan list, no plan open | `enter open  esc wait  tab agent  ?` | `enter open  esc wait  tab agent  ? help  ↑↓ move  type to find` |
| plan list, no plan open, on Create | `enter create  esc wait  tab agent  ?` | `enter create  esc wait  tab agent  ? help  ↑↓ move  type to find` |
| Find (typing) | `enter open  esc clear  tab agent` | `enter open  esc clear  tab agent  ↑↓ move` |
| Find, on `+ Create 'x'` | `enter create  esc clear  tab agent` | `enter create  esc clear  tab agent  ↑↓ move` |
| Browse, the top folder | `enter open  esc plans  tab agent  ?` | `enter open  esc plans  tab agent  ? help  ↑↓ move  type to filter` |
| Browse, deeper | `enter open  esc up  tab agent  ? help` | `enter open  esc up  tab agent  ? help  ↑↓ move  type to filter` |
| Browse, on `[ + New folder ]` / `[ + New plan ]` | `enter create  esc plans  tab agent  ?` | `enter create  esc plans  tab agent  ? help  ↑↓ move  type to filter` (`esc up` deeper) |
| Name prompt (empty) | `enter create  esc cancel  tab agent  ?` | `enter create  esc cancel  tab agent  ? help` |
| Name prompt (named) | `enter create  esc clear  tab agent` | `enter create  esc clear  tab agent` |
| Plan list, a `/word` typed | `enter help  esc clear  tab agent` | (or `enter breakdance`, `enter not a verb`) |
| Wait | `esc plans  tab agent  ? help` | `esc plans  tab agent  ? help` (`  ↑↓ scroll` when the copy runs past a short screen) |
| Outline, a task selected | `esc plans  tab agent  enter open  ?` | `esc plans  tab agent  enter open  ? help  ↑↓ move` |
| Outline, title row selected | `esc plans  tab agent  enter about  ?` | `esc plans  tab agent  enter about  ? help  ↑↓ move` |
| Outline, row 1 offers a plan | `enter switch  esc plans  tab agent  ?` | `enter switch  esc plans  tab agent  ? help  ↑↓ move` |
| Plan / Task detail | `esc outline  tab agent  ↑↓ scroll  ?` | `esc outline  tab agent  ↑↓ scroll  ? help` |
| Plan / Task detail, offer | `enter switch  esc outline  tab agent` | `enter switch  esc outline  tab agent  ? help` |
| AGENT, live | `tab plan  ? help  ↑↓ scroll` | `tab plan  ? help  ↑↓ scroll  home oldest  ctrl+c ×2 quit` |
| AGENT, empty (feed health, §10) | `tab plan  ? help  ctrl+c ×2 quit` | `tab plan  ? help  ctrl+c ×2 quit`; when the page runs past a short screen, `↑↓ scroll` comes before `ctrl+c ×2 quit` (as on Wait), and 38 cells keep `tab plan  ? help  ↑↓ scroll` |
| AGENT, scrolled (from 12 rows) | `end live  tab plan  ? help  ↑↓ scroll` | `end live  tab plan  ? help  ↑↓ scroll` |
| AGENT, scrolled (under 12 rows) | `12↑  end live  tab plan  ? help` | `12↑  end live  tab plan  ? help  ↑↓ scroll` (a 4-digit count still keeps `? help`) |
| AGENT, offer (scrolled or not) | `enter switch  tab plan  ? help` | `enter switch  tab plan  ? help  ↑↓ scroll`; with an offer `enter switch` takes `end live`'s place, since ↑↓ still reach live; under 12 rows the count leads it: `12↑  enter switch  tab plan  ? help` |
| Help | `esc close  ↑↓ scroll  tab agent` | `esc close  ↑↓ scroll  tab agent` (`tab plan` over AGENT; `↑↓ scroll` when it runs past the screen) |
| Quit armed | `ctrl+c again to quit` | replaces the footer |

Near the draft cap the count is a must-have too: `enter open  250/256  esc clear …`.

### Composer

- Only the plan list has one (§2). Every other screen gives its rows to the body.
- The box is three rows: borders plus one input row.
- Below 16 terminal rows it shrinks to a single borderless row.
- The input row shows the end of a long draft, with a `…` on the left.
- The draft cap is visible: `256/256`.

---

## 5. Glyphs, color and NO_COLOR

| Glyph | Meaning |
|---|---|
| `▸` | pinned (active, or next up) |
| `○` | ready: pending, and all deps done or skipped |
| `·` | waiting on deps; the row also shows `after <id>` while a dep is unmet (not done or skipped) |
| `✓` | done (the row is muted as well) |
| `–` | skipped |
| `!` | blocked (status `blocked`); on row 0, the other place needs you (§4) |
| `•` | on row 0, the other place has something new (§4) |
| `▌` | the selected row or card, and the outline's title row when selected |
| `[PLAN]` | the place on screen: a pill, its brackets visible only without color |
| `›` | between the crumbs of a page title (§4); `rex ›` starts Rex's own lines on AGENT (§10) |
| `×N` | on AGENT, a run of N thoughts folded to its latest (§10) |
| `✗` | on AGENT, a failed step or the turn's own error (§10) |
| `[ASK]` | on AGENT, the agent waits on you: a red pill (§10) |

**The five rules.**
1. **A fill means "here".** Only the active tab's pill, the page-title bar, the selection and AGENT's `[ASK]` pill get a fill.
2. **Each color has one meaning.** Yellow is the pin in PLAN (`▸` and its id on row 1, the outline's pinned row) and live agent work on AGENT and in the turn word (`thinking`, `running`, commands, soft drift); cyan is you (the cursor, your prompts); red needs you; green creates something; orange is a folder. Yellow is also the `REX` brand (the mascot's color) and news: the `•` cue and row 1's toasts and new-plan offer. Titles, rules and parents are not yellow, except, until the outline's own redesign, its parent rows and Task detail's title and `retrospective` rule.
3. **Three header levels, three devices.** H1 is the page title, a full-width slate bar on row 2. H2 is a bold label in capitals at column 0, a slate rule to the edge and optional muted meta (`HOW IT GOES ───`, `FEED ───`); from 16 rows a blank row goes above it; what it heads is indented 2. H3 is a muted lowercase label in an 8-cell column with its value beside it.
4. **Color never changes the characters.** Color and bold only add emphasis: a color frame stripped of its SGR is the NO_COLOR frame (a test checks every screen), except that the composer's placeholder takes parentheses without color (§2).
5. **One left edge in PLAN.** Column 0 is the selection gutter and content starts at column 2; H2 labels are outdented to column 0. Rows 0 and 1, the footer and AGENT start at column 0.

Rules:
- No glyph is an emoji, and the set avoids emoji-capable code points such as `▶`, `📌`, `⏎` and `↔`. Keys are words (§3).
- Meaning is never carried by color alone. Under `NO_COLOR` every state still reads from its glyph and its words.
- **Bold (SGR 1) is the only attribute** Rex adds: keys, the pill's word, the current crumb, H2 labels, the pin's `▸ id`, `ask` / `error`, and on AGENT the `thinking ×N` and `reply` labels, `rex ›` and a reply's headings. No dim, reverse or underline.
- Color roles come from `mascot.UI`: `title`, `text`, `muted`, `accent` (commands), `you`, `pick`, `pickbg`, `folder`, `action`, `error`, `border`. No new palettes and no new roles: text on a fill uses `pickbg` as its ink.
- Terminal.app has no 24-bit color. When `TERM_PROGRAM=Apple_Terminal` and `COLORTERM` is not `truecolor`/`24bit`, the same yellow palette is quantized to the 256-color cube and gray ramp, whichever is nearer; a pill's brackets go through the same map as its fill, so they stay invisible.

---

## 6. Outline and detail

Opening, creating or attaching a plan (§11) lands on its **Outline**.

Mockup at 120×32 (78-col text column):

```
REX [PLAN] AGENT running         following rex/plans/cuj/sendoff-demo.rex.json
▸ card-highlight  Selected plan card is one block                     1/3 done
  PLANS › Send-off demo                                                active
  last ✓ plans-ui: Home cards, browse, duplicate warn

  ✓ plans-ui         Plans navigator · Create, open, browse rex/plans in the…
▌ ▸ card-highlight   Selected plan card is one block · Bar, border, and fill…
  · load-outline     Outline view · Show tasks       after card-highlight




esc plans  tab agent  enter open  ? help  ↑↓ move
```

The same outline at 80×24 (38-col text column):

```
[PLAN] AGENT running  following …-demo
▸ card-highlight  Selected plan card…
  PLANS › Send-off demo        active
  last ✓ plans-ui: Home cards, brow…

  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan ca…
▌     Bar, border, and fill cover t…
  · load-outline   Outline view



esc plans  tab agent  enter open  ?
```

There is no composer: the rows a box would take go to the list (§2).

Layout:
- **The page title.** Row 2 reads `PLANS › <title>` with the plan's status on the right; its progress is on row 1 (§4). Once the list scrolls, ` · tasks <selected>/<total>` joins the status (e.g. `active · tasks 12/34`) while it fits. It is the title row: ↑ from the first task or Home selects it, and enter opens Plan detail (goal, stack base, scope in/out, plan problems).
- **The last line.** It shows the last finished task's `accomplished` text, the most useful "where we left off" sentence for a teammate.
- **Task order.**
  - Tasks are listed in **file order**, never topologically sorted. Moving deps around never reshuffles the list.
  - `parent` is shown as indentation, two levels at most. Deeper tasks get a `↳`.
  - A parent shows a rollup (`1/3`) instead of a status glyph.
  - **Only leaf tasks are pinned.** A parent marked `active` is only a label (§7).
- **Blurbs.** Issue #7 asks for a small blurb on every task. At text widths of 60 or more, each row shows `title · blurb` (from `does`, else `description`), clipped. Below 60, rows show the title only, and the selected row gets the blurb on its own muted line. A blurb line under every row would double the list's height at 38×18.
- **Scrolling.** The list keeps a two-row margin around the selection, and shows `↑ more` / `↓ more` when rows are hidden.
- **Plan detail** (`PLANS › <title> › about`) and **Task detail** (`PLANS › <title> › <id>`, enter or → on a task) are scrolling sub-screens under their page titles:
  - Task detail: its status (`next up` for a pin no task is `active` for), its title;
  - parent, `depends_on` (each with its state), and dependents;
  - does, needs, verify, files, branch, scope in/out, description;
  - for a done task, the retrospective shown as intended (`does`) next to actual (`accomplished`, `modified`, `verified_how`, `completed_notes`).

### Plan states

Rex never writes the plan, in any state.

| State | What you see |
|---|---|
| No tasks (a new stub) | `No tasks yet · your agent fills this file` |
| Invalid JSON | row 1: `plan JSON error 14:3 · showing last good` |
| File missing (branch switch) | `plan file missing · showing last good` |
| Conflict markers / `version` newer than Rex | `read-only: <reason>` above the tasks (a warning for whoever edits it) |
| Two `active` tasks | first one pinned; row 1 warns |
| Bad reference | `after ?T9` in the error color; cycles are tagged `cycle` and are never the pin |
| All done / nothing unblocked | `no pin · all tasks done` / `no pin · nothing unblocked` |

Reload behaviour:
- **Invalid JSON is debounced.** It shows only after it persists for two polls, because agents save files in pieces.
- **Edits** (by the agent, a teammate's pull, or your editor) reload live. They show a toast, plus one line on AGENT such as `rex › plan · plans-ui → done · +1 task` (the plan's own status as `plan → done`; `edited` when nothing else changed; §10). This is how the agent's pins and dones arrive (§7, §8). When the reload fills a plan that had no tasks, the cursor moves to the pin (§1). Reading the plan never writes it.

---

## 7. The agent pins

- **The effective pin.** It is the first `active` leaf task, else the first `pending` leaf whose `depends_on` are all `done` or `skipped`. With no tasks there is no pin, and drift is off. Rex shows it on row 1 (`▸ <id>  <title>`, with `(next up)` when it is computed) and as `▸` in the outline, and judges drift against it (§9a).
- **The agent writes it, Rex shows it.** Rex has no pin action. You ask your agent to work, continue or pick up the plan (or a task of it); the `rex-plan` skill has it set that leaf's `status` to `active`, set any other active leaf to `pending`, and the plan's own `status` to `active` if it was `pending` or `done` (never a parent's status: a parent marked `active` is only a label), and to save that first, before any other edit, so its edits are judged against the task it is on (§9a). Rex reloads the file within a poll and shows the move: a toast (`plan updated · card-highlight → active`), one `rex › plan` line on AGENT, row 1, the outline, and PLAN's cue while you are on AGENT.
- **How the agent knows the pinned task.** It re-reads the plan file at the start of every turn while it works the plan (the skill says so). Nothing is injected and no hook writes stdout, so it works the same for Claude Code, Cursor, Codex and OpenCode. You never copy anything from Rex into the agent.
- **`~/.rex/state.json`** is outside the repo and never committed. It holds only the last plan opened (or attached) per repo root: the one the next start opens (§11). An older Rex also kept pin start times there; they are ignored and dropped on the next write. Each write re-reads the file, merges, and replaces it atomically, so two Rex instances don't lose each other's entries. Opening includes start's own landing and attach: a plan Rex opened because the agent wrote it becomes the repo's last plan too.
- **Nothing is auto-claimed.** When the agent marks the pin done, Rex shows what is next up (§8), and the agent starts it only when you ask.

---

## 8. The agent marks done

Rex has no completion form. When the agent finishes the task you asked for, the skill has it fill `accomplished` (what actually happened, a retrospective rather than a copy of `does`), `modified` (paths it actually changed), `verified_how` (how it actually checked) and `completed_notes`, then set the task's `status` to `done` (in one save if it can), then set the plan's own `status` to `done` once every leaf is `done` or `skipped`. Then it stops and reports: each task is one PR.

What Rex shows when the file marks the pin done:
- Row 1: `✓ card-highlight done · next up load-outline` for a few seconds (`· now load-outline` when the same save set that task `active`; `· the plan is done` once every leaf is `done` or `skipped`, even before the agent's next save sets the plan's own `status`; else `· nothing else is unblocked`), then `▸ load-outline  Outline view (next up)`. Narrow, the done id clips from the left so the next one stays, and the plan's progress yields rather than cost it a word (§4): `✓ …ghlight done · next up load-outline` at 38 cells. Agents finish in several saves, so a later save that moves no task's status and adds or removes none (only the completion fields, only the plan's own status) keeps this toast up until it expires; a save that moves a task is news of its own.
- AGENT: one line per save, `rex › plan · card-highlight → done`, then e.g. `rex › plan · edited` or `rex › plan · plan → done`.
- The outline: `✓` on the task and its `accomplished` as the `last ✓` line; the cursor stays where you left it (§1).
- Task detail: the retrospective, intended (`does`) next to actual (`accomplished`, `modified`, `verified_how`, `completed_notes`).

---

## 9. The plan file is the hand-off

There is no send-off or planning brief, no clipboard, and nothing to paste. A new chat continues the work when you ask your agent to continue the plan ("continue rex/plans/cuj/sendoff-demo.rex.json", or just "continue the plan"); the `rex-plan` skill tells it how: read the plan file, find the current task (the `active` leaf, else the first ready one), work only on it, re-read the file every turn, and write its status and retrospective back. Without a named plan it takes the one it worked in this conversation, else the plan whose own `status` is `active`. Everything a send-off used to carry (goal, scope, tasks, what was done, where a stacked PR goes) is in the JSON, so nothing can drift from it, and Rex copies nothing.

Teaching an agent the format and this loop is `rex ingest --install-skill` (§10, docs/plan-format.md). The Wait screen says so when the skill is installed nowhere Rex knows, or a copy is from an older Rex (§11).

---

## 9a. Drift

Drift means the session is wandering off the pin. Rex only shows it. It never sends a correction, never blocks a tool, and never writes drift to the feed or the plan.

**What counts.**
- **Completed** file edits (a write that was denied doesn't count) and user prompts.
- These are ignored:
  - reads;
  - edits under `rex/plans/**`;
  - events from other repos;
  - everything, when there is no pin or the plan is empty;
  - everything, while a latched plan offers another (§11): the agent wrote that plan, so its edits and the prompts it gets are that plan's work, not drift from the latched pin.

| Level | Meaning | Examples |
|---|---|---|
| none | on the pin | a listed file, a glob or folder it lists, its `_test` sibling |
| **soft** | probably fine, worth a glance | a file next to a listed one, or in a folder under one (`testdata/`, a subpackage); a file another task lists; a prompt that names another task (not the pin's parent or what it waits on), once its turn ends with that task still not the pin (below); a long prompt that shares no words with the pin |
| **hard** | clearly off the pin | an edit or prompt that hits `scope.out`; an edit outside every folder the pin lists |

**Hitting `scope.out`.** A prompt hits an entry when it holds every word of it (short ones too: `web UI` needs both); a path hits it when it holds the entry's last word (`extra palettes` → `palettes/`) or all of its words, or matches it as a glob (`internal/mascot/**`). A compound task id (`load-outline`) in a prompt is a name, not words.

If the pin lists no files, only the `scope.out` rule applies.

**Asking for another task.** Asking the agent to start a task ("start recovery") is how the pin moves, so a prompt that names another task is held until its turn ends (an `idle` or final reply, or the next prompt). An edit of a file listed for that task waits with the prompt, since the agent may edit before it saves the plan that switches (the `rex-plan` skill has it save first). If the agent makes that task the pin first, it was the switch you asked for: nothing shows on row 1 or AGENT. If the turn ends with the pin unchanged, the prompt and those edits are recorded as soft drift then; if the pin moved to a third task, they are judged against that one.

**A batch that writes the plan.** When one batch of events both saves the open plan and edits files (parallel tool calls, a patch touching both), Rex re-reads the plan at the save: edits reported before it are judged against the plan they were made under, edits after it against what it wrote, so neither the finished task's last edit nor the new pin's first one is drift. A patch that touches the plan and code together counts each file at the lesser of the two. This holds for one save of the open plan per poll: with several in one poll (a catch-up after a pause), Rex can only read the last, so edits between them are judged against its pin.

**Display.**
- **Row 1** shows the last hard drift not yet resolved, else the last soft one: `drift · hard: README.md · outside card-highlight files` (a new-plan offer ranks between them, §4). When the plan changes (it was widened), row 1 is judged again.
- **AGENT** gets one drift block per pin, updated in place (§10): `rex › drift · <pin>`, then a row per path or prompt, hard ones in red and soft ones in yellow. Each row is one row laid out like a failed tool call: when it doesn't fit, the path or prompt clips first and `· <why>` keeps at least half the row; a path clips from the left, so its file name stays (`…/session_handler_impl.go`, or the name alone, or, when even the name doesn't fit, `…` and its end: `…_impl.go`), and a prompt from the right:

```
rex › drift · card-highlight
  hard README.md · outside card-highlight files
  soft internal/tui/live_view.go · next to plans.go
```

- Each path or prompt is listed once per pin.
- Changing the pin (or opening another plan) clears row 1 and starts a new block, even when the pin comes back to a task that had one. An on-pin edit clears a soft notice from row 1, and a hard one about that same file, though not from the block.
- Your options are yours: ask the agent to get back on the task, to pin another one, or to widen the plan (it edits the JSON; Rex reloads it), or start a fresh chat that continues the plan (§9).

---

## 10. Agent

The AGENT tab is what the agent is doing right now, as it happens. Its page title (from 12 rows) says `live`, how far you scrolled up (`12↑`) and how many lines came in below since (`12↑ · 5 new`), or `waiting for events` (§4); under 12 rows the scroll count leads the footer instead.

The stream is bottom-anchored, newest at the bottom. Each block is named by its first characters (`thinking`, `$`, `✗`, `reply`, `[ASK]`, `rex ›`, the right-aligned prompt), so it reads the same without color, and **only two blocks have a rule**: a reply (`reply ───`) and the warm-start line (`── before rex started ──`).

| Block | Looks like | Rows | Spacing |
|---|---|---|---|
| prompt | your prompt in a box at most half the width, right-aligned, its text left-aligned inside (`you`) | ≤ 6, then `(+N more rows)` | a blank row before and after |
| thought run, settled | `thinking ×N  <the latest thought, clipped>`; one thought: `thinking  <text>` (muted, the label bold) | 1 | packed |
| thought run, live | the same first row, then the rest of the latest thought, indented 2 | 2 (1 under 12 rows) | packed |
| command run | `$ <cmd>` per command, clipped (yellow); over 5: `$ … +N earlier` (muted), then the last 4, so the fold always hides at least 2 | ≤ 5 | packed |
| failed tool call (`error` with a tool) | `✗ <cmd or path> · <reason>` (red); when it doesn't fit, the command or path clips first and the reason keeps at least half the row; a path clips from the left, at a `/` or the OS's own separator (`\` on Windows, which a path outside the repo and home keeps), so its file name stays (`✗ …/view.go · permission denied`; when even the name doesn't fit, `…` and its end) | 1 | packed |
| turn error | `✗ error · <text>` (red), later rows indented 2 | ≤ 3 | packed |
| reply | `reply ───` across the row (the label bold), then the Markdown indented 2: headings bold, list items (`• `, `1. `) hanging under their text, code as muted `│ ` lines | all | a blank row before |
| ask | `[ASK] <question>`, a red pill, wrapped under the question | all | a blank row before |
| Rex: plan | `rex › plan · card-highlight → active`, `rex › plan · following <name>` (muted, `rex ›` bold) | 1 | packed |
| Rex: drift | `rex › drift · <pin>`, then `  hard\|soft <path or prompt> · <why>`, each row fitted like a failed tool call, one block per pin (§9a) | 1 + a row each | packed |
| warm start | `── before rex started ──` (muted) | 1 | no blank row after |

Mockup at 80×24 (38-col text column), the turn over, an edit next to the pin's files on row 1:

```
PLAN [AGENT] idle  following …off-demo
drift · soft: live_view.go         1/3
  AGENT                          live
                     card one block

rex › plan · card-highlight → active
thinking ×4  Looking at the plans vie…
$ … +2 earlier
$ go vet ./...
$ Edit internal/tui/plans_test.go
$ Edit internal/tui/outline.go
$ go test ./internal/tui -run PlanCar…
✗ go test ./internal/… · exit status 1
thinking ×2  The bar is drawn per row…
rex › drift · card-highlight
  soft live_view.go · next to plans.go

reply ────────────────────────────────
  The selected card is one block now.
  • plans.go draws the card through
    one painter
  • live.go starts the fill at column
    0
tab plan  ? help  ↑↓ scroll
```

Mid-turn at 80×12, the newest block a live thought run (the next command settles it to one row):

```
PLAN [AGENT] thinking  following …demo
▸ card-highlight  Selected plan…   1/3
  AGENT                          live

rex › plan · card-highlight → active
thinking ×4  Looking at the plans vie…
$ Edit internal/tui/plans.go
$ Edit internal/tui/live.go
$ go test ./internal/tui
thinking ×3  The bar is drawn per row,
  so the fill has to start at column…
tab plan  ? help  ↑↓ scroll
```

- **Live and settled.** A thought run is live while it is the newest block and the turn is in flight (`thinking`, or `running` after a tool call), scrolled or not. A drawn block under it (a command, a `✗`, an `[ASK]`, a reply) or the turn's end (even one that draws nothing, such as Cursor's `stop`) settles it to one row. A tool call that draws nothing (a file look) never settles it, and scrolling never changes the layout, so one ↑ moves the view one row and every row can be reached.
- **Folding is display only.** Only each run's latest thought shows, and only the last 4 of a run of more than 5 commands. The feed (`~/.rex/session.jsonl`) keeps every event, and Help says so. No key unfolds a run.
- **Narrow and short.** Under 30 text cells a thought run is only `thinking ×N`. Under 12 rows there are no blank rows at all, not between blocks nor inside a reply or a prompt, and no page title.
- **Scrolled.** Scrolled up, the view keeps the lines it shows: new rows arrive below it and are counted (`N new`), and a thought or command that joins a folded run adds none (but the second row a live run takes once its latest thought no longer fits on one), nor does a one-row block that settles the live run (it takes the row the run gives back); a run that settles under the view gives its row back without moving it; and the cap of 400 events trims the oldest ones above it without moving it. A scrolled view stays scrolled: at `1↑`, where the live run's first row is the view's last, a turn's end moves the view the one row the run gives back. End (or Ctrl+E) is live again.
- **Hidden.** File-look commands and result echoes stay hidden, and no file body is ever shown: a write is its path.
- **The cue.** Seen from PLAN, AGENT's cue lights `•` for a reply and `!` for an ask or a turn error, never for thoughts, commands or failed tool calls (§4).
- **Rex's own lines** (`rex ›` and the warm-start line) exist only in this process and are never appended to the feed. There are no notes: AGENT has no composer (§2).
- **Prompt boxes.** User prompt boxes are right-aligned with left-aligned text. They are capped at 6 rows, followed by `(+N more rows)`.
- **Text cleanup.** Agent text is cleaned before layout:
  - Control characters and escape/OSC sequences are removed, so agent output cannot recolor the screen, retitle the window or write the clipboard.
  - Tabs are expanded.
- **Scope.** Paths in tool lines are shown relative to the repo; replies, thoughts and prompts are shown as written. Where an event came from (its working directory or workspace roots) decides whose it is, so another repo's agent reading a file here stays hidden. Events from other repos' agents (same machine, same feed) are hidden, and the count appears under Help. An event with no working directory or roots is judged by the file it names; with nothing to go on at all, it is shown.
- **Starting mid-turn.** If the newest feed event is under 2 minutes old, the turn state is restored, so the dino is already typing. The last ~20 events from this repo are drawn under a muted `── before rex started ──` rule, laid out like the live stream (runs fold, replies get their rule), with no blank row under the rule.
- **Feed health** is the empty tab: a lead sentence, `Rex shows your agent's work here as its hooks report it.` (from 12 rows; a blank row under it from 16), then a `FEED` header (H2, §5); without a page title to say so, the header says `waiting for agent events`. The empty tab is a page, not a stream: it starts at the top and, when it runs past a short screen, scrolls from there as Wait does (§3): ↑↓, PgUp/PgDn and Home/End move it, `↑ more` / `↓ more` say what is past the screen, and only then does its footer offer `↑↓ scroll` (§4). The lead shows only whole and only with the whole block under it (the blank row goes first, then the lead), so it never starts mid-sentence, and a page that scrolls is the block alone. Help shows the same block, without the lead. The wiring is re-read every few seconds while it is on screen, so an install in another terminal shows up, and the last event follows the feed:

```
  Rex shows your agent's work here as
  its hooks report it.

FEED ───────────────────────────────────
  feed     ~/.rex/session.jsonl · last
           event 3h ago (cursor)
  cursor   wired
  claude   wired (this repo)
  codex    not wired · rex ingest
           --install codex
  skill    out of date · rex ingest
           --install-skill
  4 events from other folders hidden
```

A word wider than the value column (a long flag or path, `--install-skill` at 24 text cells) goes whole on a row of its own, hung at column 4: a command is never split mid-word.

The `skill` row says how the `rex-plan` skill stands where agents read it: `rex-plan current`, `missing · rex ingest --install-skill`, or `out of date · rex ingest --install-skill` (`… --project` when the old copy is the repo's). A copy is judged by the skill version in its generated marker (`skill v2`), never by its bytes: one with an older version, or none, is out of date; one of this version (a CRLF checkout of a committed `--project` copy too) or a newer Rex's is current, and `--install-skill` never replaces a newer Rex's copy (it says so and succeeds). Start usually lands on a plan, not the Wait screen, so this is where an upgrader sees it.

- **Installs are opt-in.** Rex never installs a hook or a skill on its own. Feed health shows the command, and you run it. The install script wires Cursor and prints the opt-in commands for the other harnesses it finds. Each one is `rex ingest --install <harness>`; `--install-cursor-hook` is an alias for Cursor's. `rex ingest --install-skill` puts the `rex-plan` skill where all four agents read it: `~/.claude/skills/rex-plan` (or `$CLAUDE_CONFIG_DIR/skills`) for Claude Code and `~/.agents/skills/rex-plan` for Codex; Cursor and OpenCode read both (and may list it twice: the copies are identical). With `$CLAUDE_CONFIG_DIR` set, Rex's copy in `~/.claude/skills`, which Cursor and OpenCode still read, is updated too, and `--uninstall-skill` removes it with the others; a `SKILL.md` there that Rex didn't write is never touched. `--project` puts both copies in the repo instead.

---

## 11. Start, attach and the plan list

Opening a plan is not a step on the happy path: the agent writes the plan, and Rex opens it.

**Landing.** Start never lands on the plan list. It opens, in this order:
1. the plan last opened in this repo, from `~/.rex/state.json`, if that file still exists and is one of this repo's plans (the same jail as attach, below);
2. else the newest `*.rex.json` under `rex/plans/**` by modification time (only at start, or while no plan is open; see the fallback below);
3. else the **Wait** screen: PLAN with no plan, under the page title `PLANS › waiting for a plan`. It has no composer (§2), nothing on it is selectable, and row 0's right side is empty. Its body, in order:
   1. the headline, verbatim and bold: `no plan yet · tell your agent to make one`, with the plan list's notice (outside git, an old `rex-plans/`) under it;
   2. `HOW IT GOES`: `1  you ask your agent to plan the work`, `2  it writes rex/plans/<name>.rex.json` (outside git too, where the notice above names the folder), `3  Rex opens it here and follows it`. A path too long for its row starts a row of its own and breaks after a `/`, never inside a name;
   3. the keys: `esc  open a plan that already exists`, `tab  watch the agent`;
   4. while the `rex-plan` skill is installed nowhere Rex knows (the user's or this repo's `.claude/skills` or `.agents/skills`, or `$CLAUDE_CONFIG_DIR/skills`), a muted hint with the command in text: `agents learn plans from the rex-plan skill · rex ingest --install-skill` when it fits on one line, else a lead over the command: `agents learn the plan format from the rex-plan skill:` where it fits a row, `agents learn plans from the rex-plan skill:` where only that does, and narrower the first one wrapped, never leaving a word alone above the command. The command is never clipped: it wraps whole. When a copy there is Rex's but an older Rex wrote it (an older skill version in its marker, or none: it still teaches that Rex's workflow), it is `rex-plan skill is out of date · rex ingest --install-skill`, or `… --install-skill --project` when the old copy is the repo's (the user's copies are judged first). With `$CLAUDE_CONFIG_DIR` set, Rex's copy left in `~/.claude/skills` counts like any of the user's: `--install-skill` updates it.

   When rows run short, these go in order: the blank rows, the key rows, `HOW IT GOES`, and the long hint, for the short one (`teach it: rex ingest --install-skill`, or `update: rex ingest --install-skill`, the command under the word where it doesn't fit beside it). The headline and the short hint never go; what still doesn't fit scrolls with ↑↓, pages and Home/End. With plans off (`rex/ is a file here · plans off`) it says only that and how to turn them on: nothing the agent writes can open until the file moves. AGENT works as always; Esc or ← is the plan list, where Create stays as an escape hatch, and while no plan is open Esc or ← from there comes back.

```
[PLAN] AGENT idle

  PLANS › waiting for a plan

  no plan yet · tell your agent to
  make one

HOW IT GOES ──────────────────────────
  1  you ask your agent to plan the
     work
  2  it writes
     rex/plans/<name>.rex.json
  3  Rex opens it here and follows it

  esc  open a plan that already exists
  tab  watch the agent

  agents learn the plan format from
  the rex-plan skill:
  rex ingest --install-skill



esc plans  tab agent  ? help
```

Either plan lands on its outline and is **following**. The plan list's notice, which start no longer shows, is a row-1 toast on the first frames instead. An upgrader never sees the Wait screen's skill line, so when the skill is out of date that line is the toast instead (it wins over the notice, which the plan list still shows), and Help's feed block keeps a `skill` row (§10, §13). A missing skill is no toast: the Wait screen and Help say so.

**Following and latched.** Row 0 says which (§4).
- *Following* (unlatched): Rex opens the plan file the agent writes. Every start is following.
- *Latched*: you picked another plan in the plan list (a recent, a match, a Browse row, Open latest, or one you created there). That is the only way to latch: Rex has no other action on a plan. enter on Continue, or on any row that names the open plan, picks nothing new and keeps a followed plan followed. Rex keeps showing a latched plan; the agent's write of another plan only puts `new plan <name> · enter switch` on row 1 (and PLAN's cue, seen from AGENT), and enter on the outline, a detail screen or AGENT switches to it and follows again. enter takes it only while row 1 shows it (the plan file's error, a toast or hard drift cover it, §4). Only drift judged before the offer appears can cover it, an edit in the same poll as the write that brings it included: while it stands, nothing is judged against the latched pin (§9a). Taking it opens a plan, so it also waits for what holds an attach (below): while the plan list's composer has text or a Name prompt is open, enter keeps its usual meaning and row 1 doesn't offer it. Opening a plan clears the offer. A write held while you pick or create a plan in the plan list becomes the offer, neither opened nor lost, unless it is the plan you picked.

**Attach.** Rex follows a hook event, never a guess. In a batch of events, after they are judged for drift, the last one that passes all of these is opened:
- a completed file edit (a Write, Edit or afterFileEdit result, a Codex or OpenCode `apply_patch`), not a denied or failed write, never a read or a shell command;
- from this repo's agent (§10, Scope);
- of an existing regular `*.rex.json` under this repo's `rex/plans/`, once the path is cleaned and its symlinks resolved: a `..` counts by where it leads, and a link out of `rex/plans/` leads nowhere (a relative path is taken from the event's working directory);
- not the plan already open, judged by the file rather than its spelling (another case on macOS or Windows, a link inside `rex/plans/`). A write of the open plan only reloads it (§6) and never hides another plan written in the same batch or patch: that one is still opened or offered.

Following, it opens the plan's outline (as opening it in the plan list would: §6, §7). Row 1 says `following <name>`, PLAN's cue lights if you are on AGENT, and AGENT gets one muted line, `rex › plan · following <name>`, so the move stays on record (Rex's own line, never the feed). The place never changes. Text in the plan list's composer or a Name prompt (you are finding or making a plan), in either place, holds the attach: row 1 says `new plan <name> · opens when the line is clear` (`· opens when the prompt closes` while a Name prompt is open, §4), and the plan opens once they clear (Esc, an emptied draft, the prompt cancelled). Nothing else holds it: the Wait screen, the outline, the details, AGENT and Help have no composer, so letters typed there leave nothing behind. A newer write replaces a held one; a write of the open plan drops it (the agent is back on that plan); picking or creating a plan in the plan list makes it the row-1 offer (above), unless it is the plan you picked. Keys read together with a batch of events are applied first, so a key acts on the screen you saw and a find typed with it holds the attach. The plan's first write may be a stub; the outline fills from the 500 ms reload, and the cursor moves to the pin once tasks appear (§1). A plan that doesn't parse when attach opens it shows its error at once, as any first open does; the two-poll debounce is for a plan already open (§6).

**Fallback.** While no plan is open, Rex also looks for a plan file once a poll (an editor save, or a write whose hook was missed) and opens the newest. Once a plan is open, attached or latched, Rex never scans the folder to change the screen: only a write is followed, and newest-by-mtime is never used again.

The plan list (`PLANS`, with how many plans there are as its meta):

- **Cards, in order:**
  1. **Continue `<plan>`**: the plan that is open now. Esc from the Outline lands here, and enter on it keeps a followed plan followed. (Start already reopens the last plan, above.)
  2. **Create a plan**.
  3. **Open a plan**.
  4. **Open latest**: the newest plan by modification time.

  Recents follow. The cursor starts on Continue, else Open latest, else Create.
- **No plans yet.** Only Create shows, plus `plans are JSON in rex/plans/ · commit them` and, with no plan open, `or ask your agent to write rex/plans/<name>.rex.json · Rex opens it`. Start shows the Wait screen instead of this; Esc from it lands here, and Esc or ← goes back.
- **Hints** show progress on the cards, recents, Find results and Browse rows, e.g. `2/5 · ▸ card-highlight`. The currently open plan reads `open · 2/5 · ▸ card-highlight` in recents, Find and Browse.
- **Find (typing on Home)** matches across `rex/plans/**`.
  - The first match is selected.
  - The list always ends with `+ Create 'x'` (unless a plan is named exactly `x`). With no match, that row is selected, and enter creates the plan at once and opens its outline. There is no extra prompt.
- **Browse** (`PLANS › rex/plans/`, then `› cuj/` for each folder down; its meta counts the plans in that folder and below, and with a filter the plans under the folders and plans it leaves):
  - The cursor starts on the first folder or plan, and a filter selects its first match.
  - `[ + New folder ]` / `[ + New plan ]` sit **below** the listing.
  - Entering or leaving a folder clears the filter.
  - enter on a non-plan file says `not a .rex.json`.
  - With nothing matching, it shows `no match for 'x' in rex/plans/sub`.
- **Name prompt:**
  - `a/b` creates the plan inside folder `a`, and the target path is shown before enter.
  - Duplicates warn and never overwrite.
  - A new plan is a valid v1 stub, not `{}`.
- **Errors** clear on the next key.
- **Starting Rex never deletes anything, and never creates a plan file or `rex/plans/`.** The plan start opens is recorded in `~/.rex/state.json` (§7), which may create `~/.rex`. Following or showing a plan never writes the plan file; the only plan Rex ever writes is the new empty stub you create here (never over an existing file).
  - A file named `rex` at the repo root shows `rex/ is a file here · plans off` instead of being removed.
  - Outside a git repo, Home says `not in a git repo · plans go to <cwd>/rex/plans`.
  - Plans left in an old `rex-plans/` folder are reported (`rex-plans/ has 2 old plan(s) · move them into rex/plans`), never moved.
  - `rex/plans/` is created on the first Create or New folder.

---

## 12. Layout tiers

| Terminal | Layout |
|---|---|
| ≥ 66 cols | mascot (40) + gutter (2) + text column (≥ 24) |
| < 66 cols | **text only**: the mascot is hidden and the text column uses the full width |
| text column ≥ 42 cols | row 0 starts with `REX` (120 columns, or a text-only terminal of 42–65); narrower, the mascot is the brand |
| text column < 30 cols | an AGENT thought run is only `thinking ×N`, without its latest thought (§10) |
| text column < 24 cols | `REX · widen to 24 cols`; keys other than Ctrl+C and Ctrl+L are ignored |
| < 16 rows | the plan list's composer is one borderless row; no blank row goes above an H2 |
| < 12 rows | AGENT has no page title: its body takes row 2, and its scroll count leads the footer (`12↑  end live …`). It has no blank rows, and a live thought run takes 1 row instead of 2 (§10) |
| < 7 rows | the plan list's composer is hidden: printable keys and pastes are ignored there, so nothing is typed blind, and enter never submits its draft. Its footer says only what works: `type to find` / `type to filter` go, every enter goes but `enter open` on an empty draft (enter still opens the selected row), and `? help` goes while a hidden draft has text. Screens without a composer keep their keys and footers; `?` on an empty draft, and on screens without a composer, still opens Help |
| `TERM=dumb`, pipes, CI | the static card (`rex start --static`) |

Body rows under the page title:

| Terminal rows | PLAN screens / Help | plan list (with its composer) | AGENT |
|---|---|---|---|
| 24 | 20 | 17 | 20 |
| 16 | 12 | 9 | 12 |
| 12 | 8 | 7 | 8 |
| 7 | 3 | 2 | 4 (no page title) |

The page title is chrome: it is never a body line, so the lines that stay put above a list and the selection margin never see it. A selected row or card taller than the rows its list has (a plan-list card at 7 rows) shows its top, so its title is never the part cut.

Notes:
- **No full clears.** Rex never clears the whole screen on a normal frame. Each changed row is erased from column 1 and then written whole, inside synchronized-output markers. Nothing is erased after a row: after a write to the last column the cursor stays there, and an erase would wipe that cell. The cursor is hidden while painting.
- **Repaints.** Resize, `SIGCONT` and Ctrl+L force a full repaint.
- **Widths.**
  - Text is clipped by **cell width**: East Asian wide characters and emoji count 2, combining marks 0.
  - All East-Asian-ambiguous characters count 1, including `·`, `…`, `↑↓`, `○`, `▌`, `•`, `›`, `×`, `→` and box drawing. "Ambiguous characters are wide" terminal settings are not supported.
  - The width table is a small range list written by hand in `internal/tui`. The standard library has no width data, and no new module is added for it.

---

## 13. Help overlay (`?`)

`?` opens it at once from any screen (§2); `/help` then enter does too in the plan list. Its page title is the crumbs of the page under it, then `HELP` (`PLANS › Send-off demo › HELP`), and it keeps its place across Tab. It keeps `THIS PAGE` and its scroll while it is open: a plan the agent writes meanwhile opens under it (§11) without changing them, and Esc shows it. The crumbs follow the page under Help, so they always name where Esc goes (§4). Its sections, each under an H2:
1. `THIS PAGE`, with the page's name as meta (`outline`, `about`, `task`, `plans`, `folder`, `new plan`, `new folder`, `waiting`, `agent`): the keys of the page it was opened over. On `agent` a note under the keys, with no key beside it, says that folding is display only: the feed keeps every thought (§10).
2. `EVERYWHERE`: `tab` (switch plan / agent), `?`, `ctrl+l`, `ctrl+c ×2`.
3. `MARKS`: the plan glyphs of §5 (`▸`, `○ · ✓`, `– !`, a parent's `1/3`, the cursor `▌`), the cue (`•` `!`), and what `following` / `latched` mean (§11). AGENT's marks are under `AGENT TAB`.
4. `AGENT TAB`: how AGENT names its blocks (§10): `thinking` (a run of N thoughts, `×N`), `$`, `✗`, `[ASK]`, `reply ───`, `rex ›`.
5. `PLAN LIST`: what typing does there (§2), `a/b`, Ctrl+U / Ctrl+W, `?` or `/help`, and `/breakdance`.
6. `REX NEVER`: runs or approves tools, edits a plan (it only creates an empty one you name in the plan list), sends anything to your agent, or reads transcripts; your agent pins and finishes tasks in the file, and Rex shows it.
7. `FEED`: the feed path and feed health (§10), with the `skill` row (whether the `rex-plan` skill is current, missing or out of date, and the command to fix it), and the hidden-event count.

`THIS PAGE` lists every key its page takes, not only its footer's: on the outline, About and Task detail, enter's switch to a new-plan offer, in AGENT's words (`switch PLAN to a new plan row 1 offers`); Home and End, with Ctrl+A and Ctrl+E (§3), wherever they move or scroll, Browse and Wait included; PgUp/PgDn on Wait. A test holds each page to the keys that act on it.

The key column is 11 cells wide, keys in bold; under 34 cells, or for a key that fills the column, the key sits above what it does. No key is an icon (`↔`, `⏎`): keys are words.

It has no composer: letters do nothing in it. ↑↓ and pages scroll it, Esc closes it, Tab leaves it open in its place.

---

## 14. Keystroke budget

The full-loop tests check these counts.

| Step | Keys |
|---|---|
| Open the plan you had last time | 0 (start opens it) |
| Create a plan | 0 in Rex: ask the agent to write `rex/plans/<name>.rex.json`; Rex opens its outline |
| Open a plan by name | Esc `name` enter (latches it) |
| Create a plan in Rex | Esc `name` enter via find → `+ Create 'name'`; lands on its outline (latched) |
| Switch to a plan the agent wrote while latched | enter |
| See the pin | 0 (row 1) |
| Watch the agent | Tab |
| Back to the plan | Tab |
| Pin a task | 0 in Rex: ask your agent |
| Mark the pin done | 0 in Rex: ask your agent |
| Continue in a fresh chat | 0 in Rex: ask the new chat to continue the plan (§9) |
| Help | `?` |
| Quit | Ctrl+C Ctrl+C |

---

## 15. Transitions

Each state's page title (§4) names it; AGENT under 12 rows has only its tab.

| State | Page title | Entered via | Left via |
|---|---|---|---|
| Wait | `PLANS › waiting for a plan` | start with no plan to open · Esc/← from the plan list's Home while no plan is open | an attach (the agent writes a plan, or the fallback finds one) → Outline · Esc/← → plan list Home · `?` · Tab |
| plan list Home | `PLANS` (`PLANS › find` while finding) | Esc from Wait, from Browse root or from Outline (cursor on Continue) | enter on a card, recent or match (latches another plan; Continue keeps following) · enter on `+ Create 'x'` creates and opens the outline · typing (find) · Esc/← → Wait while no plan is open · an attach while following (with an empty find) · `?` on an empty find · Tab |
| Browse | `PLANS › rex/plans/ › …` | enter on Open a plan | enter on a plan → Outline · Esc/← · an attach while following (with an empty filter) · Tab |
| Name prompt | `PLANS › new plan` / `new folder` | Create card · `[ + New plan ]` / `[ + New folder ]` rows | enter → Outline (plan) or into the new folder · Esc (nothing written) · Tab (name kept). An attach waits until it closes, and creating a plan makes it the row-1 offer |
| Outline | `PLANS › <title>` | start (the last or newest plan) · an attach · open or create a plan · enter on a new-plan offer · Esc from detail · Tab back | enter/→ detail (enter switches while row 1 offers a new plan) · Esc/← → plan list · an attach of another plan while following · `?` · Tab |
| Plan detail | `PLANS › <title> › about` | enter on the title row | Esc/← · an attach of another plan while following · enter on a new-plan offer · `?` · Tab |
| Task detail | `PLANS › <title> › <id>` | enter/→ on a task | Esc/← · an attach of another plan while following · enter on a new-plan offer · `?` · Tab |
| Help | `<the page under it> › HELP` | `?` anywhere (in the plan list, on an empty draft) · `/help` enter in the plan list | Esc · Ctrl+C |
| AGENT | `AGENT` (from 12 rows) | Tab | Tab · `?` · enter on a new-plan offer (PLAN switches, you stay) |
| Quit armed | – | Ctrl+C | Ctrl+C quits · any key / timeout disarms |

---

## 16. Friction register

This is what the 2026-10 audit found, from four lenses: first run, daily loop, teammate handoff, and terminal robustness. Each item lists its symptom, the fix, and the slice that owns it. Code locations are as of `dev` @ `4318b56`. Rows marked **design** are risks in the planned features, not bugs on `dev`.

| ID | Sev | Friction (symptom) | Fix | Slice |
|---|---|---|---|---|
| F1 | critical | Start deletes a file named `rex`, `RemoveAll`s `rex-plans/` after a name clash, and creates `rex/plans/` in whatever non-git folder you start in, e.g. `$HOME` (plans.go:76-122) | Never delete; create lazily; show notices instead (§11) | `fix/plans_dir_safety` |
| F2 | high | Quitting can leave the TTY broken: Windows Ctrl+C kills the process; no SIGTERM/SIGHUP restore | Windows raw input mode; signals take the restore path (§3) | `fix/key_input` |
| F3 | high | Esc does nothing on Windows (0x1B is not mapped); emoji split into two `�` (surrogate halves) | Map it; join surrogate pairs | `fix/key_input` |
| F4 | high | Fast arrows or the wheel drop keys and type stray `A`/`B`; paste submits; a multi-byte character at the 256 cap leaves a lone byte; Ctrl+S freezes output | Buffered tokenizer, bracketed paste, cap by runes, IXON/IEXTEN off | `fix/key_input` |
| F5 | high | Agent text is painted raw: escape/OSC sequences can repaint the screen or set the clipboard | `CleanText` at the render boundary (§10) | `fix/text_sanitize` |
| F6 | high | Below 66 cols the text column vanishes, but keys still act | Layout tiers (§12) | `feature/ux_chrome` |
| F7 | high | Local composer text renders as `── reply ──`; the pane is called "chat" | Local `note` kind; "tape"; no echoed verbs (§2). *Replaced:* the tape has no composer and no notes (F36) | `feature/ux_input` |
| F8 | high | Browse: typing a filter then ⏎ opens "New folder"; the filter leaks into subfolders | Selection follows the filter; `+` rows below; clear on folder change (§11) | `fix/library_navigation` |
| F9 | high | Creating or opening a plan shows nothing of the plan; files are `{}` | v1 stub; land on the outline (§6) | `feature/plan_schema`, `feature/plan_outline` |
| F10 | high | An empty plan dead-ends the loop | Empty state + planning brief (§9). *Replaced:* the agent writes and fills the plan, taught by the skill (F37) | `feature/sendoff` |
| F11 | high | The global feed mixes repos; paths are absolute or cwd-relative | cwd/session meta; repo-relative paths; repo scope (§10) | `feature/event_scope` |
| F12 | high | No data for `modified`/`verified_how` (results never reach the log) | Evidence from raw events + feed since pin start (§8). *Replaced:* the agent fills the completion fields itself (F37) | `feature/task_completion` |
| F13 | high | The empty tape can't say whether any hook is wired | Feed health (§10) | `feature/harness_hooks` |
| F14 | med | Turn state is wrong: Claude never idles; a tool failure idles mid-turn; restart shows idle | Cursor `stop` → idle and tool errors keep the turn running (`fix/cursor_installer`); map Claude Stop (`feature/harness_hooks`); warm start (`feature/event_scope`) | `fix/cursor_installer`, `feature/harness_hooks`, `feature/event_scope` |
| F15 | design | Outline hotkeys would collide with the composer and with paste | ⏎-only verbs (§2). *Replaced:* no verbs and no composer outside the plan list; keys act at once (F36) | `feature/ux_input` |
| F16 | med | Esc and Backspace navigate inconsistently (Esc on Home jumps to the tape) | Esc ladder; Backspace only deletes (§3) | `feature/ux_input` |
| F17 | med | The plans header drops turn state and path; the open plan is invisible in the library | Header on every screen; Continue card (§4, §11) | `feature/ux_chrome`, `feature/plan_outline` |
| F18 | med | Home: inert composer, dead cards, sticky errors, Open latest takes ↓↓⏎ | Find on Home; hide dead cards; clear errors; default cursor (§11) | `fix/library_navigation` |
| F19 | med | Lists have no scroll position; selection is a row index | Scroll offset + margin; selection by identity (§1, §6) | `fix/library_navigation` |
| F20 | med | Footers are cut mid-word at 80 cols | Ranked segments (§4) | `feature/ux_chrome` |
| F21 | med | The existing write pattern reorders keys and isn't atomic; agents edit the same file | Byte-span edits, hash re-check, atomic writes. *Replaced:* Rex no longer writes plans at all, only a new stub (F37) | `feature/plan_store` |
| F22 | design | Live-reload edge states are undefined | Debounce, last good, read-only states (§6) | `feature/plan_outline` |
| F23 | design | Pin semantics in a shared file (claiming, blocked, parents) | §7. *Replaced:* the agent pins, by the skill's rules; Rex only shows the effective pin (F37) | `feature/pin_task` |
| F24 | med | Copy can claim success falsely; pasted briefs flood the tape | Honest copy; plain print; fingerprint collapse; 6-row cap (§9, §10). *Replaced:* nothing is copied or pasted; the plan file is the hand-off (F37). The 6-row cap stays | `feature/sendoff`, `feature/clipboard` |
| F25 | design | Drift would be noisy and shown in too many places | Row 1 + one block per pin; soft/hard with plain reasons (§9a) | `feature/prompt_drift` |
| F26 | design | A dense outline overflows 38×18 | §6 layout | `feature/plan_outline` |
| F27 | med | Every frame clears the screen: flicker, wasted CPU | Diff renderer + caches (§12) | `fix/render_flicker` |
| F28 | med | The tail drops replies that grow (the last line is rewritten in place), and a poll mid-rewrite replays the whole feed onto the tape | Append-only feed | `fix/ingest_fail_open` |
| F29 | med | 24-bit color only; Terminal.app on macOS ≤ 15 has none | 256-color quantization of the same palette (§5) | `fix/render_flicker` |
| F30 | med | Tab drops a half-typed name; `a/b` silently becomes `b` | Drafts per place; subfolder create (§11). *Replaced:* only the plan list has a draft, kept across Tab (F36) | `feature/ux_input`, `fix/library_navigation` |
| F31 | low | No Ctrl+U/W; silent 256 cap; stale "press again" footer | §2, §3 | `feature/ux_input` |
| F32 | low | Nothing is discoverable (breakdance only in the README) | Help overlay, placeholders (§13) | `feature/ux_input` |
| F33 | low | Width counted in runes: CJK titles break borders | Cell-width clipping (§12) | `fix/text_sanitize` |
| F34 | low | README:26 says "Teammates pick the same plan and watch the same send-off", but each machine only sees its own agent | Reword | `feature/plan_schema` |
| F35 | low | No trace after quitting | Exit line (§3) | `feature/ux_chrome` |
| F36 | high | A chat-style composer on every screen is impractical when the only input is a single letter and ⏎ (2026-10-23 feedback) | The composer exists only in the plan list; keys act at once elsewhere; `?` is a single key (§2, §3) | `feature/agent_drives` |
| F37 | high | Pinned-task context and the send-off had to be copied from Rex into the agent by hand (2026-10-23 feedback) | The agent drives the plan: it pins, marks done and continues from the file, as the `rex-plan` skill teaches; Rex only shows it and never writes it (§7–§9) | `feature/agent_drives` |
| F38 | high | Which page you are on isn't evident: the footer's first word and a `── rule ──` were the only names (2026-10-24 feedback) | A page title on row 2, a breadcrumb that is also the Esc ladder; the footer no longer names the screen (§4) | `feature/chrome_tabs` |
| F39 | high | How to switch places isn't evident, and "tape" doesn't say what it is (2026-10-24 feedback) | Both tabs always on row 0, `[PLAN] AGENT`, the one on screen a pill; "tape" is AGENT; a cue says the other tab has news (§1, §4) | `feature/chrome_tabs` |
| F40 | med | Section headers blend into the text; no visual hierarchy (2026-10-24 feedback) | H1 page title bar, H2 capitals with a rule, H3 field labels; bold as the one added attribute (§5) | `feature/chrome_tabs` |
| F41 | med | Footer keys read as icons (`⏎`) in ` · `-joined lists that name the screen and miss `? help` at 80×24 (2026-10-24 feedback) | Keys only, as words (`enter`, `esc`), bold, two spaces apart, rank = display order with short forms; `? help` and Esc's target fit 38 cells (§4) | `feature/chrome_tabs` |
| F42 | high | AGENT is crowded with thinking spam: a `── thinking ──` rule over every run, each thought wrapped in full, a rule on every block (2026-10-24 feedback) | A run of thoughts is one row, `thinking ×N` and its latest (two while live); command runs over 5 fold to `$ … +N earlier` and the last 4; a failed step is one `✗` row; only a reply and the warm start keep a rule, the rest are named by their first characters; blank rows only around prompts and before replies and asks, none under 12 rows; folding is display only (§10) | `feature/agent_stream` |

Found in the same audit, outside the UI:

| ID | Sev | Problem | Fix | Slice |
|---|---|---|---|---|
| H1 | critical | `rex ingest` exits 2 on a bad flag or source; Claude Code, Cursor and Codex treat exit 2 as **block** | Ingest always exits 0 with empty stdout; stdin and lock deadlines | `fix/ingest_fail_open` |
| H2 | high | The Cursor installer overwrites `hooks.json` it can't parse; writes non-atomically; quotes with Go syntax; registers shell commands twice | Safe merge, `.bak`, atomic write, shell quoting | `fix/cursor_installer` |
| H3 | med | Every hook call reads the whole feed file | Seek-based tail read | `fix/ingest_fail_open` |
| H4 | med | After the feed is truncated, the tail keeps a stale partial line and glues the next event to it, which is then dropped | Clear the pending fragment on truncation | `fix/ingest_fail_open` |
