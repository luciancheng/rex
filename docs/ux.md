# REX terminal UX spec

This is the target design for the REX CLI loop:

the agent writes a plan (Rex opens it) → the agent pins a task → watch → see drift → the agent marks it done → quit

The agent drives the plan; Rex only shows it. Rex has no pin, done, send-off or brief actions: you ask your agent, it writes `active` and `done` (and the completion fields) into the plan JSON, as the `rex-plan` skill teaches, and Rex reloads and shows it. The only thing you type in Rex is a find, filter or name in the plan list.

It records each decision, the friction it removes, and which slice of work owns it. TUI changes cite the section they implement. `AGENTS.md` is still the contributor contract. Where this spec changes an `AGENTS.md` rule, the change that ships it updates `AGENTS.md` too.

Everything here keeps the product rules:
- watch-only, plus one thing Rex hands over: the explicitly pinned task, added to each prompt as context (§7a);
- the plan JSON is the source of truth, and Rex never writes it (except a new empty plan you create in the plan list);
- yellow only, 40×40 poses;
- hooks only;
- Rex sends the agent nothing else: never a prompt, an approval or a correction.

---

## 1. Places

Rex has exactly **two places**. **Tab** switches between them and does nothing else.

| Place | Screens |
|---|---|
| **PLAN** | Wait (no plan yet) · plan list (Home, Find, Browse, Name prompt) · Task list · Task details · About |
| **AGENT** | what the agent is doing, live (§10) |

**Every screen has a title on row 0**: where you are as a breadcrumb (`Send-off demo › card-highlight`, `Plans › cuj/`, `Agent`), and at its end a quiet label naming both places, `plan · agent`, the one on screen bright (§4). The breadcrumb is the Esc ladder: Esc goes to the crumb on its left, and the footer's Esc word names it (§3, §4). There is no tab bar and no cue: from AGENT only your agent's question or its turn's error reaches PLAN, as a notice that holds while it still applies or until you look at AGENT (§4), and nothing moves you, your selection or your scroll.

**Overlays** open on top of whichever place you are in. There is one: Help. Esc closes it, and Tab leaves it open in its place. (Completion, Send-off and Pin confirm are gone, with the actions they served: the agent writes the plan, §7–§9.)

Each place remembers its screen, its selection, its scroll position and any open overlay; PLAN also keeps the plan list's draft, the only one there is (§2). Tab away and back, and everything is where you left it, unless an attach opened another plan in PLAN meanwhile (below, §11).
- Selection is kept by **identity**: plan path or task id. Row indexes are not used.
- A reload, resize or rebuild keeps the same plan or task selected. If that item disappears, the selection falls back to the pin, then to the nearest row. One more exception: when a plan with no tasks (or one that didn't parse) gains its first tasks, the selection goes to the pin, as opening the plan does.

**Events never move you, with one exception.** Hook events, plan reloads and notices change what is drawn. They never change the place, selection or draft. Only keys do, apart from the two selection fallbacks above and **attach** (§11): while the session is unlatched (*following*), a completed write of a plan file in this repo's `rex/plans/` opens that plan's task list in PLAN, and so does a plan file the cold-start scan finds while no plan is open. Even then the place never changes (on AGENT you stay on AGENT, and the notice that names it waits for PLAN), and text in the plan list's composer or an open Name prompt holds the attach until it clears; the notice row says it is waiting. A latched session is never moved: the new plan is only offered on the notice row, where `o` opens it (§3, §4), and taking it waits for the same things.

---

## 2. Input: the composer is only in the plan list

Rex has no actions of its own on a plan, so most screens have nothing to type. The **composer** exists only in the plan list, where text finds, filters or names a plan:

| Screen | Composer means | Prefix | Empty placeholder (dim) |
|---|---|---|---|
| plan list Home | find a plan across `rex/plans/**` | ` / ` | `find or create a plan` |
| Browse | filter this folder | ` / ` | `type to filter` |
| Name prompt | plan or folder name | ` + ` | `plan name (a/b for a folder)` / `folder name` |

The Wait screen, the task list, task details, About, AGENT and Help have **no composer box**; their body takes those rows. There, keys act at once (§3), `o` and the task list's `i` among them, and every other printable key and every paste does nothing: no hidden draft is built, so nothing typed there can ever act later.

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

Keys are written as words everywhere Rex shows them: `enter`, `esc`, `tab`, `?`, `↑↓`, `home`, `end`, `ctrl+c`, `o`, `i`.

| Key | plan list Home | Browse | Name prompt | Task list | Task details / About | AGENT | Help |
|---|---|---|---|---|---|---|---|
| ↑↓ PgUp/PgDn | move | move | – | move between tasks (2-row margin; group labels skipped) | scroll | scroll 1 line / 1 page | scroll |
| Home / End | first / last | first / last | – | first / last task | top / bottom | oldest / live | – |
| Ctrl+A / Ctrl+E | as Home / End | as Home / End | – | as Home / End | as Home / End | oldest / live | – |
| enter | open the selected plan or folder, or start `+ New plan` / `+ New folder`, or create from `+ Create 'x'`; a `/word` runs it | open the plan or enter the folder | create, then open its task list (plan) or enter it (folder); empty: "type a name first" | the task's details | nothing | nothing | nothing |
| o | with an empty draft and a new plan on the notice row, open it; else type | type | type | open the plan on the notice row | open the plan on the notice row | open the plan on the notice row in PLAN (you stay on AGENT) | nothing |
| → | enter the selected folder | enter the folder | – | the task's details | – | – | – |
| i | type | type | type | About | – | – | – |
| Esc | clear the find text, else back to Wait while no plan is open, else nothing | clear the filter, else up one folder (from a top folder, to the plan list) | clear the name, else cancel (nothing written) | to the plan list (cursor on the plan, under **OPEN**) | back to the task list, on the same task | nothing | close |
| ← | (empty draft) back to Wait while no plan is open | (empty draft) up one folder (from a top folder, to the plan list) | – | to the plan list | back to the task list, on the same task | – | – |
| `?` | Help, when the composer is empty | Help, when the filter is empty | Help, when the name is empty | Help | Help | Help | nothing |
| other letters, paste | type | type | type | nothing | nothing | nothing | nothing |
| Tab | to AGENT | to AGENT | to AGENT (name kept) | to AGENT | to AGENT | to PLAN | Help stays open in its place |
| Ctrl+L | full repaint | full repaint | full repaint | full repaint | full repaint | full repaint | full repaint |
| Ctrl+C | clear + arm quit | clear + arm quit | clear + arm quit | arm quit | arm quit | arm quit | close + arm quit |

The Wait screen (§11) has no composer either: ↑↓, PgUp/PgDn and Home/End (or Ctrl+A/Ctrl+E) scroll its copy when it runs past a short screen; Esc or ← goes to the plan list's Home; `?` opens Help; enter and letters do nothing. AGENT's empty tab (feed health, §10) is a page too and scrolls the same way: there Home/End (and Ctrl+A/Ctrl+E) are its top and bottom.

**`o` opens a new plan your agent wrote** (§11), the plan the notice row offers (`Your agent wrote Password reset (auth/reset) · o opens it`, §4), and follows again. It acts only while the notice row shows that offer and names `o` (an error, your agent's question or a toast above it hide it) and opening it would lose nothing: not while the plan list's composer has text or a Name prompt is open (§11), nor in Browse or inside Help. Elsewhere `o` does what any letter does there: types in the plan list, nothing on the other screens. **enter never takes an offer**: it keeps its one meaning on every screen. `o` never joins the footer; the notice names it.

In the plan list, Esc first clears a non-empty draft. Otherwise it goes **up one level within the place**, to the crumb on the left of the title (§4), and the footer names where: `esc plans`, `esc tasks`, `esc up`, `esc clear`, `esc cancel`, `esc close`, and `esc wait` on the plan list while no plan is open.
- Task details or About → the task list, on the same task → plan list Home (`Send-off demo › card-highlight`, `Send-off demo › About`; the task list's title is the plan alone, and its Esc is `esc plans`).
- Wait → plan list Home → Wait, while no plan is open: Esc (or ←) takes you back and forth, so the Wait screen is never lost, and the plan list's footer says `esc wait`. With a plan open, Home is the top.
- Browse → parent folder → the plan list, on the folder you came from (`Plans › cuj/ › sub/`); `../` climbs the same way. There is no Browse root: from a top folder Esc is `esc plans`.

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

Every screen has the same frame: where you are on row 0, what the page is on row 1, what just happened on the row above the footer, and the keys that act here on the last row.

| Row | Content | 12+ rows | 7–11 rows |
|---|---|---|---|
| 0 | **Title row**: the breadcrumb, the page's meta, the places' label | ✓ | ✓ |
| 1 | **Context row**: what the page is; on the task list, the pin line | ✓ | ✓, but a notice takes it while one shows |
| 2 | blank; while the open plan's list or page is scrolled, `↑ N more` at its right end (§6) | ✓ | – |
| 3… | Body; the plan list's composer at its bottom (below) | | |
| last−1 | **Notice row**: one message, or blank | ✓ | – |
| last | **Footer**: keys | ✓ | ✓ |

The chrome starts at column 0 of the text column and ends one cell short of its edge. The text column is used up to **100 cells**: on a wider terminal the rest of each row stays empty. There is no tab bar, no cue and no turn word on PLAN: the title names the page, the label names the places, and the footer names the way to the other one (`tab agent`, `tab plan`).

### Row 0: title row

```
Send-off demo                             1/3 done · following   plan · agent
Send-off demo › card-highlight                          pinned   plan · agent
Plans › cuj/ › sub/                                     1 plan   plan · agent
Send-off demo      1/3   plan · agent
card-highlight  pinned   plan · agent
```

- **The breadcrumb**, from column 0: the current crumb in the bright bold `title`, the crumbs before it and the ` › ` between them muted. **It is the Esc ladder**: Esc goes to the crumb on the left (from the task list, to the plan list), and the footer's Esc word names it (§3). Crumbs are never re-cased.
- **The meta**, muted, right-aligned before the label: what the page holds or where it stands (an error's word red, the pin's state yellow).
- **The places' label**, three cells after the meta, at the row's end: `plan · agent`, the place on screen in bright `text`, the other place and the `·` muted. It never blinks or changes color with news: only your agent's question or its turn's error reaches PLAN, on the notice row (below).
- **The cursor never selects row 0.** On the task list the plan's title is chrome, like every title: About is on `i` (§6).

| Screen | Crumbs | Meta | Esc word |
|---|---|---|---|
| Wait | `Plans › Waiting for a plan` | `<repo> · no plans yet` (`· N plans`) | `esc plans` |
| plan list | `Plans` | `<repo> · 4 plans` (outside git, the folder: `~/notes · 4 plans`); short, `4 plans` | `esc wait` while no plan is open, else – |
| Find | `Plans › Find` | `1 match` / `N matches` | `esc clear` |
| Browse | `Plans › cuj/` (deeper: `› sub/`) | plans in that folder and below; with a filter, those under the rows it leaves | `esc plans` from a top folder, else `esc up` |
| Name prompt | `Plans › New plan` / `New folder` (from Browse: `Plans › cuj/ › New plan`) | where it creates: `rex/plans/`, `rex/plans/cuj/` | `esc cancel` (`esc clear` with a name) |
| Task list | the plan's title (its file stem if untitled) | `1/3 done`, the `done` tasks of those the list shows (`no tasks`), then ` · following` while Rex follows the agent's writes (latched shows no word, §11), ` · read-only`, ` · can't read` (red); short, `1/3` | `esc plans` |
| Task list, unreadable | its file stem | `can't read` (red) | `esc plans` |
| About | `<title> › About` | `active · 1/3 done` | `esc tasks` |
| Task details | `<title> › <task id>` | its state: `pinned` (the pin's yellow), `next up`, `✔ done`, `pending`, `blocked`, `skipped`; `also active` and `cycle` red | `esc tasks` |
| AGENT | `Agent` | `live`; `↓ 12 newer` while scrolled up (the rows below the view); `no events yet` | – |
| Help | the crumbs of the page under it, then `Help` | – | `esc close` |

**When it doesn't fit**, these go in order: the breadcrumb clips first, down to its current crumb (middle crumbs clip to 6 cells each with `…`, then the root becomes `…`, then the middle crumbs fold into it, then the current crumb stands alone); then the meta goes, its short form first; then the label shortens to the place on screen (`plan`); under 30 text cells there is no label, and the breadcrumb and meta take the row. Only the current crumb is ever cut short, and only when even it doesn't fit beside the label.

### Row 1: context row

Muted, one line, saying what the page is:

| Screen | Row 1 |
|---|---|
| Task list | **the pin line**: `pinned  card-highlight · Selected plan card is one block` (the id the pin's yellow, the title `text`); with nothing active, `nothing pinned · next up load-outline · Outline view` (never yellow: next up is never sent, §7a); `nothing pinned · nothing unblocked`; `nothing pinned · no tasks yet`; `✔ all tasks done` (`✔` green). Narrow, the title clips with `…`; with fewer than 8 cells for it, the line is `pinned  <id>`. Its right side, where the pin's send state goes, is empty for now |
| Task list, unreadable · About | the plan's path, `rex/plans/cuj/sendoff-demo.rex.json` |
| Task details | where the task is: `task 2 of 3`, then `· in <group>` and `› <parent>` for each parent it is under (`task 4 of 9 · in Plan views › Plan list`) |
| plan list | the plan list's notice (outside git: `not in a git repo · plans go to ~/notes/rex/plans`; an old `rex-plans/`; plans off), fitted (§11); else `Pick up where you left off, or start a new plan.`, or with no plans `No plans here yet.` |
| Find | `Plans whose name or title has "send".` |
| Browse | `Folders, then plans. Type to filter.` |
| Name prompt | `Name it. a/b puts plan b in folder a.` (a folder: `… puts folder b in folder a.`) |
| Wait | the plan list's notice, else `Rex opens the plan your agent writes.` |
| AGENT | the turn: `idle`, `thinking`, `running`, `waiting on you` (`text`), `turn error` (red); on the right, while a plan is open, `pinned <id>` (the id the pin's yellow) or `nothing pinned`, which goes before the turn word would clip; the empty tab: `Rex shows your agent's work here as its hooks report it.` |
| Help | `Keys for the task list, then everywhere.` |

### The notice row

The row above the footer shows one message at a time, the highest kind that applies; when it ends, the next one waiting shows. Under 12 rows there is no notice row: a notice takes row 1 while it shows, so the footer always stays.

| # | Kind | Example | Where | Lasts |
|---|---|---|---|---|
| 1 | an error, red | `✗ plan JSON error 14:3 · showing last good` · `✗ plan error: permission denied · showing last good` (the cause, never the file's path, §6) · `✗ plan file missing · showing last good` · the plan list's `✗ '/x' is not a verb · try /help` · `✗ cuj/next is gone` | both places (the plan list's on the plan list) | while it holds; the plan list's until the next key; a failed open 4 s |
| 2 | your agent needs you | `Your agent is asking you something · tab agent to read it` · `✗ Your agent's turn ended with an error · tab agent` (red) | PLAN | an ask until the turn goes on past it (as row 1's turn word does on AGENT: a command, a result, a reply, the turn's end or the next prompt, whatever AGENT draws), a turn's error until the next prompt; either until you look at AGENT. Nothing is raised while AGENT is on screen |
| 3 | a toast, `text` (`✔` green) | `plan updated · plans-ui → done · +1 task` (the plan's own status reads `plan → done`) · `✔ card-highlight done · next up load-outline` (§8) · which file opened (§6): `Opened Send-off demo · rex/plans/cuj/sendoff-demo.rex.json`, `Your agent wrote Password reset · opened rex/plans/auth/reset.rex.json`, `✔ Created rex/plans/team/auth.rex.json` · at start, `rex-plan skill is out of date · rex ingest --install-skill` or the plan list's notice (§11) | PLAN | 4 s; one raised while AGENT is on screen waits, and its 4 s start when you look at PLAN |
| 4 | a new plan (§11), `text` | `Your agent wrote Password reset (auth/reset) · o opens it`; held, `… · opens when the line is clear` / `… · opens when the prompt closes` | both places | until it is taken, replaced, or a plan is opened |
| 5 | hard drift (§9a) | `off pin · README.md · outside card-highlight files`, `off pin` in the pin's yellow, the rest `text` | PLAN | until the pin changes, an edit of that file is on the pin, or you look at AGENT (drift that happens while AGENT is on screen raises none) |
| 6 | read-only | `read-only: version 2 is newer than this Rex` (muted) | the open plan's screens | while that plan is open |

Narrow, each keeps what it is about: an open's path stays and clips from the left (`Opened …ans/cuj/demo.rex.json`); the done toast's id clips from the left first (`✔ …ghlight done · next up load-outline`), then its tail shortens; a new plan's title goes first, then its path clips from the left (`…demo`) down to 5 cells, then `· waiting` stands in for a held one's reason, then `new plan` for `Your agent wrote`. `· o opens it` shows only where `o` takes the offer (§3): not while the plan list's composer has text or a Name prompt is open, in Browse or in Help.

### Last row: footer

The footer lists **keys**, never the screen's name: the title says where you are. It is one muted line of `key verb` segments joined by ` · `: `↑↓ select · enter details · esc plans · tab agent · ? help`.

- Keys are words: `↑↓`, `enter`, `esc`, `tab`, `?`, `i`, `home`, `end`, `ctrl+c`, and `type to find` / `type to filter` where letters type. Never `⏎` or `↔`.
- A segment shows only if its key acts on that screen. `enter` names where it goes, and its verb follows the selection (`enter open` / `enter create`; on the task list always `enter details`); `esc` always names its target (`esc plans`, `esc tasks`, `esc up`, `esc clear`, `esc cancel`, `esc close`, `esc wait`).
- **The footer never changes as the cursor moves.** Each segment is fitted at the width of its longest form on that screen (`enter create`, `enter details`), so which segments show never depends on the row selected; only a verb changes.
- Segments drop by rank, the display order never changes: `? help` last and always kept, then `enter`, `esc`, `tab`, `↑↓`, typing, AGENT's `end live` and `home oldest`, and the task list's `i about` first of all.
- `o`, which opens a plan your agent wrote, never joins the footer: the offer's notice names it.
- After the first Ctrl+C the footer is `ctrl+c again to quit`, muted.
- Under 7 rows the plan list's hidden composer takes no typing (§12): `type to …` goes, `enter` goes while a hidden draft has text and in a Name prompt, and `? help` while a hidden draft has text.

| Screen | 24 cells | 38 cells | 79 cells (121 columns) |
|---|---|---|---|
| plan list | `enter open · ? help` | `enter open · tab agent · ? help` | `↑↓ select · enter open · type to find · tab agent · ? help` |
| plan list, no plan open | `enter create · ? help` | `enter create · esc wait · ? help` | `↑↓ select · enter create · type to find · esc wait · tab agent · ? help` |
| Find | `enter open` | `enter open · esc clear · tab agent` | `↑↓ select · enter open · esc clear · tab agent` |
| Browse, a top folder | `enter open · ? help` | `enter open · esc plans · ? help` | `↑↓ select · enter open · type to filter · esc plans · tab agent · ? help` (`esc up` deeper) |
| Name prompt (empty) | `enter create · ? help` | `enter create · esc cancel · ? help` | `enter create · esc cancel · tab agent · ? help` |
| Name prompt (named) | `enter create` | `enter create · esc clear · tab agent` | `enter create · esc clear · tab agent` |
| plan list, a `/word` typed | `enter help · esc clear` | `enter help · esc clear · tab agent` | `enter help · esc clear · tab agent` (or `enter breakdance`, `enter not a verb`) |
| Wait | `esc plans · ? help` | `esc plans · tab agent · ? help` | `esc plans · tab agent · ? help` (`↑↓ scroll ·` first while its copy runs past the screen) |
| Task list | `enter details · ? help` | `enter details · esc plans · ? help` | `↑↓ select · enter details · i about · esc plans · tab agent · ? help` (60 cells: without `i about`) |
| Task list, no tasks | `esc plans · ? help` | `esc plans · tab agent · ? help` | `i about · esc plans · tab agent · ? help` |
| Task details, About | `esc tasks · ? help` | `esc tasks · tab agent · ? help` | `↑↓ scroll · esc tasks · tab agent · ? help` |
| AGENT | `tab plan · ? help` | `↑↓ scroll · tab plan · ? help` | `↑↓ scroll · home oldest · end live · tab plan · ? help` |
| AGENT, empty (feed health, §10) | `tab plan · ? help` | `tab plan · ? help` | `tab plan · ? help` (`↑↓ scroll ·` first while it runs past the screen) |
| Help | `esc close · tab agent` | `↑↓ scroll · esc close · tab agent` | `↑↓ scroll · esc close · tab agent` (`tab plan` over AGENT; `↑↓ scroll` while it runs past the screen) |
| Quit armed | `ctrl+c again to quit` | `ctrl+c again to quit` | `ctrl+c again to quit` |

An offer changes no footer. Near the draft cap the count is a must-have, after what enter does: `enter open · 250/256 · esc clear …`. A test pins every screen's footer at 24, 38, 60 and 79 cells, and another moves the cursor over every row of every list screen.

### Composer

- Only the plan list has one (§2), at the bottom of its body, over the notice row. Every other screen gives its rows to the body.
- The box is three rows: borders plus one input row.
- Below 16 terminal rows it shrinks to a single borderless row.
- The input row shows the end of a long draft, with a `…` on the left.
- The draft cap is visible: `256/256`.

---

## 5. Glyphs, color and NO_COLOR

| Glyph | Meaning |
|---|---|
| `✔` | done, always green (the row is muted as well) |
| `–` | skipped (the row is muted) |
| `!` | blocked (status `blocked`) |
| (blank) | pending: ready, or waiting on another task (its details say `Waits on`) |
| `pinned` | the task list's tag on the task the agent set `active`, in the pin's yellow; its state on its details' row 0 (§6) |
| `next up` | the tag on the task Rex would pin while none is active, muted: it is never sent (§6, §7a) |
| `also active` · `cycle` | red tags: a second `active` leaf, a task in a loop (§6) |
| `▸ ○ ·` | in the plan list's accordion only: the pin, ready, waiting on another task (§11) |
| `❯` | the selection, in the 2-cell gutter at column 0, on one row of the screen: a list's selected row (in the plan list, the row of the plan opened in place, §11) |
| `+` | an action that creates: `+ New plan`, `+ New folder`, `+ Create 'x'` |
| `›` | between the crumbs of row 0's title (§4); `rex ›` starts Rex's own lines on AGENT (§10) |
| `·` | between the facts on one row, and between the footer's keys (§4) |
| `×N` | on AGENT, a run of N thoughts folded to its latest (§10) |
| `✗` | on AGENT, a failed step or the turn's own error (§10) |
| `[ASK]` | on AGENT, the agent waits on you: a bold pill (§10) |

**The five rules.**
1. **Nothing is filled.** No background color is ever painted in the text column (no SGR 48; the mascot's half-block pixels are its own palette). The selection is `❯` in the gutter and the selected row's text in purple (`pick`); on it the marks that carry meaning keep their color: `✔` its green, the `pinned` tag its yellow, a red tag its red. A plan opened in place in the plan list has `❯` and purple on its own row only; its lines under it keep their roles.
2. **Each color has one meaning.** Yellow (`accent`) is only the pin: the agent's `active` task, which is what reaches the agent (§7a) — its id on row 1's pin line and on AGENT's row 1, its `pinned` tag on the task list (the only yellow there) and its `pinned` state on its details' row 0, the `▸ <id>` in the plan list's accordion — and hard drift's `off pin`, which is off the pin; plus the mascot's own palette. Purple (`pick`) is only the selection. Green (`action`) is only `✔`. Red (`error`) is only an error: a turn's error, a failed step, `can't read`, a plan's problems, read-only, `also active`, a cycle or a bad reference. Everything else is `text` or `muted`: a computed next up (never sent, so never yellow), titles, crumbs, the places' label, rules, group labels, page labels, commands, prompts, folders, `+` actions, toasts and offers, the footer, a block, soft drift, an ask.
3. **Three header levels, three devices.** H1 is row 0's title: the current crumb in the bright bold `title`, the rest muted. H2 is a bold label in capitals at column 0, a slate rule to the edge and optional muted meta (`HOW IT GOES ───`, `FEED ───`); from 16 rows a blank row goes above it (on the plan list, only where the selected plan keeps its lines with it, §12); what it heads is indented 2. A page's labels (task details, About) are muted, in a 14-cell column, with their values beside them in `text` (§6); a group label on the task list and a task's title on its details are bold `text`. Titles are never yellow.
4. **Color never changes the characters.** Color and bold only add emphasis: a color frame stripped of its SGR is the NO_COLOR frame (a test checks every screen), except that the composer's placeholder takes parentheses without color (§2). Under NO_COLOR `❯` still says where you are and `✔` what is done.
5. **One left edge in PLAN.** Column 0 is the selection gutter and content starts at column 2; H2 labels are outdented to column 0. The chrome (rows 0 and 1, the notice row and the footer) and AGENT start at column 0.

Rules:
- No glyph is an emoji, and the set avoids emoji-capable code points such as `▶`, `📌`, `⏎` and `↔`. Keys are words (§3). The one exception is `✔` (U+2714), which defaults to text presentation: Rex prints it bare, never with U+FE0F, and counts it as one cell; if a terminal draws it two cells wide, `✓` is a one-line swap with the same meaning.
- Meaning is never carried by color alone. Under `NO_COLOR` every state still reads from its glyph and its words.
- **Bold (SGR 1) is the only attribute** Rex adds: the current crumb, the pill's word, H2 labels, a group label on the task list, a task's title on its details, `ask` / `error`, and on AGENT the `thinking ×N` and `reply` labels, `rex ›` and a reply's headings. No dim, reverse or underline.
- Color roles come from `mascot.UI`: `title` (240,244,250, bold), `text`, `muted`, `accent` (the pin), `pick` (175,135,255; xterm 141 under 256 colors), `action` (`✔`), `error`, `border` (rules, the composer). No new palettes and no new roles; `pickbg`, `folder` and `you` are retired.
- Terminal.app has no 24-bit color. When `TERM_PROGRAM=Apple_Terminal` and `COLORTERM` is not `truecolor`/`24bit`, the same yellow palette is quantized to the 256-color cube and gray ramp, whichever is nearer.

---

## 6. Task list and details

Opening, creating or attaching a plan (§11) lands on its **task list**, and the notice row says which file opened (§4): `Opened Send-off demo · rex/plans/cuj/sendoff-demo.rex.json`; `Your agent wrote Password reset · opened rex/plans/auth/reset.rex.json` when your agent's write opened it; `✔ Created rex/plans/team/auth.rex.json` for a plan you made in the plan list. Narrow, the path stays and clips from the left (`Opened …ans/cuj/demo.rex.json`). The cursor lands on the pin, so the one purple row and the one yellow tag meet. One focus per screen: the list asks which task, a task's details say what is in it, and nothing previews the next screen.

The landing state at 80×16, the cursor on the pin:

```
Send-off demo                               1/3 done · following   plan · agent
pinned  card-highlight · Selected plan card is one block

  ✔ plans-ui        Plans navigator
❯   card-highlight  Selected plan card is one block                      pinned
    load-outline    Outline view









↑↓ select · enter details · i about · esc plans · tab agent · ? help
```

A plan with groups (a top-level parent and the tasks under it):

```
Rex visual redesign                                     2/6 done   plan · agent
pinned  page-titles · Page titles and footer keys

  Chrome: tabs, page titles, keys                                           1/2
    ✔ tab-bar      Tab bar on row 0
❯     page-titles  Page titles and footer keys                           pinned

    agent-stream   Fold runs of thoughts

  Plan list and outline                                                     0/2
      plan-list    Plan list groups and accordion
    ! pin-card     Pinned card on the outline

  – width-table    Glyph widths in width_test

↑↓ select · enter details · i about · esc plans · tab agent · ? help
```

Narrow (38×12), a row is its id and tag:

```
Send-off demo      1/3   plan · agent
pinned  card-highlight · Selected pl…

❯ ✔ plans-ui
    card-highlight             pinned
    load-outline





enter details · esc plans · ? help
```

There is no composer: the rows a box would take go to the list (§2). Under NO_COLOR every frame has the same characters: `❯`, `✔ – !` and the words of the tags carry every meaning.

### The list

Row 0 is the plan's title with its progress and how it got here (§4); the cursor never selects it. Row 1 is the pin line (§4). The body is one list and nothing else: no goal, card, blurbs or `TASKS` rule (the goal is on About and in each task's details, as `Plan goal`).

- **Order.** Tasks are listed in **file order**, never topologically sorted, so moving deps around never reshuffles the list. A row is a leaf task; a repeated id shows only its first task. **Only leaf tasks are pinned**: a parent marked `active` is only a label (§7).
- **A row** is the 2-cell gutter (`❯` on the selection), the glyph, the id in one column (the longest id with its indent, + 2, 8 to 18 cells; a longer id clips with `…`), the title, and one tag at the right edge, a cell short of it. Titles clip first, then ids; the tag stays. Under 40 text cells a row is its id and tag.
- **Glyphs**: `✔` done (green, the row muted), `–` skipped (the row muted), `!` blocked, and blank for the rest: pending, ready or waiting (details say what it `Waits on`).
- **Tags**: `pinned` on the agent's pin, in the pin's yellow, the only yellow on the list; `next up` on the task Rex would pin while nothing is, muted (it is never sent, §7a); `also active` on a second active leaf and `cycle` on a task in a loop, red (a problem in the plan). A task has one tag at most.
- **Groups.** A top-level parent is a **group label**: its title bold, its rollup (`1/3`) muted on the right. The cursor skips it, so enter never lands on a heading. Its tasks are indented 2 (their glyph moves; the `❯` doesn't); a deeper parent has no line of its own, its tasks sit in the group in file order, and their details name it (`in Plan views › Plan list`). A task in a parent loop has no top to sit under: it is a row of its own at the top, tagged `cycle`, and its details name no group. From 12 rows a blank row goes above each group and before ungrouped tasks that follow one, only when the whole list fits; under 12 rows there are no labels and no blank rows, and the indent keeps the grouping.
- **The selection** is `❯` in the gutter and the row's text in purple, with no fill; its `✔` stays green and its tag keeps its color.
- **The cursor** lands only on task rows: ↑↓, PgUp/PgDn and Home/End never stop on a label, a blank row or a scroll marker. Home is the first task, End the last.
- **Scrolling.** The list keeps a two-row margin around the selection (one in a short window) and moves only when the cursor would leave it. `↓ N more` takes the list's last row while it goes on below; `↑ N more` sits at row 2's right end from 12 rows, costing no row, and takes the list's first row under 12. N counts the rows past the window. PgDn selects the task just past the window's last and PgUp the one just before its first, so a page never skips a task the window didn't show; with no task that way, the last or the first.
- **It holds still.** Whether it has blank rows between groups is decided without the selection, so moving the cursor never changes the rows, only which one has `❯`.
- **Counting.** Row 0's `2/8 done` counts the `done` tasks of those the list shows (a skipped task isn't done, and a repeated id is one row); About's `Progress` leads with the same count.
- **Cost.** Each read of the plan is indexed once: every task's depth (a walk up stops at the first task it already knows, so a deep chain of parents costs its length), the list's lines and the keys the cursor reaches. A key never walks the plan again, a frame draws only the lines on screen, and a row reads only a row's cells of a task's id and title, so a 4 MB plan lays out in linear time.

### Details

**enter** or **→** on a task opens its **details** on a page of their own; **Esc** or **←** goes back to the list on the same task. Row 0 is the breadcrumb `<title> › <id>` with the task's state as meta (`pinned` in the pin's yellow, `next up`, `✔ done`, `pending`, `blocked`, `skipped`; `also active` and `cycle` red); row 1 places it: `task 2 of 3`, then `· in <group>` and `› <parent>` for each parent it is under.

The body is the task's title in bold, one muted line, then a definition list: a muted label in a 14-cell column and its values in `text`, each value starting a row and wrapping under it, keeping the file's line breaks (blank lines dropped). Under 50 body cells a label sits above its values. Empty fields are left out; there are no blank rows.

**The fields are the pin note's** (§7a), in its order, **as the note sends them** (`plan.SentFields`, the same pass `plan.PinNote` words, so the page can't promise more than the agent gets): `Also active` (on the pin only, red), `Does`, `Description`, `Needs`, `Waits on` (`<id> (not done)`), `Verify`, `Files`, `Builds on` (`<id> (done): <what it accomplished>`), `Out of scope` (the plan's and the task's), `Branch` (`feature/x, stacked on feature/y`), `Plan goal`. Each value is cleaned and folded onto one line and cut as the note cuts it (160 characters, the description 300, the goal 200); a list shows the note's first values and `(+N more in the file)`. A field the note drops to stay under its 1,500 bytes still shows, whole as the file has it, but muted, and a muted line under the first names them: `Not sent (the note is capped at 1,500 bytes): Out of scope, Plan goal`. The muted line under the title says whether the fields reach your agent, wrapped like the title: `Your agent gets these fields with every prompt.` on the agent's pin; `Once this task is pinned, your agent gets these fields with every prompt.` on any other task; `A read-only plan sends your agent nothing.`, and then every field is muted.

```
Send-off demo › card-highlight                            pinned   plan · agent
task 2 of 3

  Selected plan card is one block
  Your agent gets these fields with every prompt.
  Does          Bar, border and fill cover all three rows of the selected card
  Needs         the pick and pickbg colors exist in mascot.UI
  Verify        go test ./internal/tui -run PlanCardSelection
                Open latest: ▌ on all three card rows
  Files         internal/tui/plans.go
                internal/tui/live.go
                internal/tui/plans_test.go
  Builds on     plans-ui (done): Home cards, folder browser, duplicate names
  ↓ 6 more

↑↓ scroll · esc tasks · tab agent · ? help
```

**A done task** leads with what your agent wrote when it finished it: `✔ Done · what your agent wrote when it finished it` (`✔ Done · no notes yet` with no completion fields), then `Did`, `Changed`, `Checked` and `Notes` (`accomplished`, `modified`, `verified_how`, `completed_notes`), then what was planned, muted: `Planned` (its `does`), `Description`, `Verify`, `Files`, `Branch`.

```
Send-off demo › plans-ui                                  ✔ done   plan · agent
task 1 of 3

  Plans navigator
  ✔ Done · what your agent wrote when it finished it
  Did           Home cards, folder browser, duplicate names refused
  Changed       internal/tui/plans.go
                internal/tui/plans_test.go
  Checked       go test ./internal/tui -run Plan (passed)
  Planned       Create, open and browse rex/plans in the TUI
  Verify        go test ./internal/tui -run Plan
  Files         internal/tui/plans.go
```

### About (`i`)

**i** on the task list (an empty plan's too) opens **About**: row 0 `<title> › About` with `active · 1/3 done`, row 1 the plan's path. Its body is the same kind of definition list: `Goal`, `In scope`, `Out of scope`, `Progress` (row 0's count first: `2/8 done · 1 skipped · 1 blocked · 4 to go`, `no tasks yet`), `Stack` (`lands on dev · 7 branches`, in a stacked plan), then `Problems` and `Read-only`, in red, when there are any. **Esc** or **←** goes back to the list on the same task.

```
Send-off demo › About                          active · 1/3 done   plan · agent
rex/plans/cuj/sendoff-demo.rex.json

  Goal          A teammate opens this plan in Rex and sees what the work is and
                which task the agent is on; a fresh chat picks it up from the
                plan file without rereading any transcript.
  In scope      plan cards in the Library · selection highlight
  Out of scope  extra palettes · web UI
  Progress      1/3 done · 2 to go
```

Details, About, and a plan's page when it has no list (no tasks, or none Rex could read), scroll (↑↓, PgUp/PgDn, Home/End) with the list's markers, `↓ N more` on their last row and `↑ N more` on row 2's right end (under 12 rows, their first row). Each is laid out once per read of the plan, task and width, so a frame that only scrolls a long page costs the rows it shows. enter does nothing on them.

### Plan states

Rex never writes the plan, in any state.

| State | What you see |
|---|---|
| No tasks (a new stub) | `No tasks yet.`, then `Ask your agent to plan <name>. It writes the tasks into <path>, and they show up here as it saves.`; nothing to select, so the footer is `i about · esc plans · tab agent · ? help` (`↑↓ scroll ·` first while the page runs past a short screen); row 1 `nothing pinned · no tasks yet` |
| Invalid JSON | the notice row: `✗ plan JSON error 14:3 · showing last good`; row 0's meta `· can't read`; the list stays usable from the last good copy |
| Unreadable (no permission, a folder, too big) | the notice row: `✗ plan error: permission denied · showing last good`, the cause and never the file's absolute path; with no good copy yet the body says `Can't read this plan.`, why, and `Rex shows it as soon as the file is valid JSON again.`, row 0 is its file stem and `can't read`, row 1 its path |
| File missing (branch switch) | `plan file missing · showing last good` |
| Conflict markers / `version` newer than Rex | `read-only: <reason>` on the notice row and `· read-only` in row 0's meta (§4); its pin's details say `A read-only plan sends your agent nothing.` |
| Two `active` tasks | the first is the pin; the second is tagged `also active`, red |
| Bad reference / a loop | About's `Problems`; a task in a loop is tagged `cycle`, red, and is never the pin; a task in a parent loop is a row of its own at the top |
| All done / nothing unblocked | row 1: `✔ all tasks done` / `nothing pinned · nothing unblocked` |

Reload behaviour:
- **Invalid JSON is debounced.** It shows only after it persists for two polls, because agents save files in pieces.
- **Edits** (by the agent, a teammate's pull, or your editor) reload live. They show a toast, plus one line on AGENT such as `rex › plan · plans-ui → done · +1 task` (the plan's own status as `plan → done`; `edited` when nothing else changed; §10). This is how the agent's pins and dones arrive (§7, §8): the tags follow the pin. The selection stays on the same task by id; a task that left, or became a group, gives way to the pin, and when the reload fills a plan that had no tasks, the cursor moves to the pin (§1). Reading the plan never writes it.

---

## 7. The agent pins

- **The effective pin.** It is the first `active` leaf task, else the first `pending` leaf whose `depends_on` are all `done` or `skipped`. With no tasks there is no pin, and drift is off. Rex shows it, and judges drift against it (§9a), in four places: row 1's pin line (`pinned  <id> · <title>`, or `nothing pinned · next up <id> · <title>` when it is computed, §4); the task list's tag on its row, `pinned` for the agent's `active` task or `next up` for a computed one (§6); its details' row 0, where its state says the same, and whose fields are the note the agent gets (§6, §7a); and the plan list's accordion (`pinned` / `next up`, §11). The agent's `active` task is in the pin's yellow in all four; a computed pin is never sent (§7a), so it is never yellow (§5).
- **The agent writes it, Rex shows it.** Rex has no pin action. You ask your agent to work, continue or pick up the plan (or a task of it); the `rex-plan` skill has it set that leaf's `status` to `active`, set any other active leaf to `pending`, and the plan's own `status` to `active` if it was `pending` or `done` (never a parent's status: a parent marked `active` is only a label), and to save that first, before any other edit, so its edits are judged against the task it is on (§9a). Rex reloads the file within a poll and shows the move: a toast (`plan updated · card-highlight → active`), one `rex › plan` line on AGENT, row 1 and the list's tags; seen from AGENT, the toast waits for PLAN (§4).
- **How the agent knows the pinned task.** Before each prompt, Claude Code's, Codex's and Cursor's hooks and OpenCode's plugin hand it the pinned task as a short note, read from the plan file just then (Cursor: on builds that read it, and when each chat starts; OpenCode: from plugin v2, §7a). It also re-reads the plan file at the start of every turn while it works the plan (the skill says so), so the file stays the source of truth. You never copy anything from Rex into the agent.
- **`~/.rex/state.json`** is outside the repo and never committed. It holds only the last plan opened (or attached) per repo root: the one the next start opens (§11), and the only plan whose pin the prompt hooks hand the agent (§7a). An older Rex also kept pin start times there; they are ignored and dropped on the next write. Each write re-reads the file, merges, and replaces it atomically, so two Rex instances don't lose each other's entries. Opening includes start's own landing and attach: a plan Rex opened because the agent wrote it becomes the repo's last plan too.
- **Nothing is auto-claimed.** When the agent marks the pin done, Rex shows what is next up (§8), and the agent starts it only when you ask.

---

## 7a. The pin reaches the agent

Pinning a task has to do something for the agent, or it loses focus (F47). So on a **prompt**, while the plan Rex last opened in this repo has an **explicit pin** (an `active` leaf), the prompt hook's `rex ingest` prints a short note that the harness adds to the agent's context (OpenCode has no prompt hook that reads stdout: its plugin asks `rex ingest` for the note and adds it itself). In every other case ingest is what it always was: no stdout, exit 0. A computed next up is never sent, and **nothing pinned, nothing sent** is also the opt-out, so there is no config flag.

**The lookup** runs in `internal/cli` (ingest) and calls `internal/plan` (`PinFolder`, `PinFor`, `PinNote`); `internal/watch` only decides which hooks ask, which folders their agent works in (`ContextFolders`, from the harness's own fields) and the shape of each harness's reply, so `watch` never imports `plan`.
1. **The folder.** Walk up from the payload's `cwd` (OpenCode's plugin sends its project's directory) to `.git`, never the hook's own folder; outside git, the payload's `cwd`. A payload without an absolute `cwd` gets nothing. Cursor runs user hooks from `~/.cursor` and its payloads name the workspace instead, so for Cursor the folder is the first of its `workspace_roots`, then `$CURSOR_PROJECT_DIR`, that is in a git repo; outside git, the first of them. Only absolute paths count; with none, nothing is sent.
2. **The plan.** Only `~/.rex/state.json`'s last plan for that folder (a key that spells the same folder another way counts, and of those the one opened last wins), through the path jail (cleaned, symlinks resolved, under `rex/plans/`, a regular `*.rex.json` of at most 4 MB). What you see in Rex is what the agent gets: no other plan is read, however pinned, so in a repo where you never opened Rex, a plan committed with an `active` task (a teammate's, or the demo plan `rex/plans/cuj/sendoff-demo.rex.json` in Rex's own repo) reaches no agent. No entry, or one the jail turns away: nothing is sent, no guessing. A read-only plan (a newer `version`, conflict markers) sends nothing, and so does a plan that can't be read (it may be mid-save or mid-merge with a pin inside). A plan whose path the note can't name as it is (over 160 characters, or with a run of spaces the note would fold) sends nothing either: a cut path names no file.
3. **The task.** The first `active` leaf, the one Rex shows as pinned. Another `active` leaf is named in the note as `Also active` (a plan problem the person sees too).
4. **A new plan.** When the last plan has no tasks yet, the note is one line instead, the same at the start of a session (below): `Rex note: the person has rex/plans/team/auth.rex.json open in Rex; it has no tasks yet.` A last plan with tasks and no pin sends nothing, whatever another plan pins.
5. **The deadline.** 750 ms, counted from the process's start (Codex runs hooks through `$SHELL -lc`, whose startup is already spent from its 5 s). Missed: no output, exit 0, and the prompt is still recorded, without `meta.pin`.

**The note** is factual, not imperative (imperative injected text trips prompt-injection defenses; the how-to lives in the skill), and always starts with `Rex note`, so Codex never sees `{` or `[` first. For the CUJ fixture:

```text
Rex note, as of this prompt: the task pinned in the person's plan, read just now from rex/plans/cuj/sendoff-demo.rex.json. The plan file is the source of truth; earlier Rex notes in this conversation are out of date.
Pinned task: card-highlight · Selected plan card is one block (status active, in plan "Send-off demo")
Does: Bar, border and fill cover all three rows of the selected card
Needs: the pick and pickbg colors exist in mascot.UI
Verify: go test ./internal/tui -run PlanCardSelection; Open latest: ▌ on all three card rows
Files: internal/tui/plans.go, internal/tui/live.go, internal/tui/plans_test.go
Builds on: plans-ui (done): Home cards, folder browser, duplicate names refused
Out of scope: extra palettes; web UI
Plan goal: A teammate opens this plan in Rex and sees what the work is and which task the agent is on; a fresh chat picks it up from the plan file without rereading any transcript.
The person wants this turn to serve this task and stay inside its files and scope. If the prompt asks for something outside it, they expect you to say so before editing. Edits outside these files show in Rex as drift.
```

- Lines without a value are left out. Optional lines, when true: `Also active: <ids>` after the task line, `Description: …` after `Does`, `Waits on: <ids> (not done)` after `Needs` (a pin can be blocked).
- `Out of scope` joins the plan's and the task's `scope.out`. `Builds on` is `Graph.BuildsOn`, which the plan list's accordion uses too. Task details show these fields in this order, as the note sends them (`plan.SentFields`, the same pass, §6): what the cap drops is muted there and named, so the person reads what the agent gets. `Branch` is the task's `branch` and, in a stack, the branch it stacks on (`Graph.StackBase`, docs/plan-format.md, Stacks); a name that isn't plain is never handed on, and when the nearest earlier `done` leaf has no plain branch, no base is named (an older one's would leave its commits out). With no `files`, the last line asks only for its scope and says nothing of drift.
- Every value goes through `watch.CleanText` and is folded onto one line; `<system-reminder` and `</system-reminder`, in any case, become `‹system-reminder` and `‹/system-reminder`.
- Caps: each item 160 characters (a joined one, `Builds on`'s `<id> (done): <what>` or `Branch`'s `<branch>, stacked on <base>`, as a whole); lists show 4 items, then `(+N more in the file)`; the goal 200, the description 300; the whole note **1,500 bytes**, cut at a line boundary by dropping `Plan goal`, then `Branch`, `Builds on` and `Out of scope`, then list items from the end, a round at a time from the last list up, each list's first item only once no list has a second, and `Description` only once no list has an item left. The first two lines and the last always stay; only the task line is cut, last of all.
- **At the start of a session** (Claude's and Codex's `SessionStart` with `source: "compact"`, after a compaction, and Cursor's `sessionStart`, when a chat starts) the first line reads `Rex note, as of the start of this session: the task pinned in the person's plan, read from <path>. The pin can move during the session; the plan file has the current one.`

**Per harness.**

| Harness | Events (all registered before, except Cursor's `sessionStart`) | stdout, then exit 0 | Silent when |
|---|---|---|---|
| Claude Code | `UserPromptSubmit`; `SessionStart` with `source: "compact"` | `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"<note>"}}` and a newline (`"SessionStart"` after a compaction), hidden from the person | the payload has `cursor_version` (Cursor running Claude's hooks) or `agent_id` (a subagent); no pin; the deadline |
| Codex | `UserPromptSubmit`; `SessionStart` with `source: "compact"` | the note as plain text and a newline, which Codex adds as developer context | `agent_id`; no pin; the deadline; never on `Stop`, `PermissionRequest` or any other event (plain text is invalid there) |
| Cursor | `beforeSubmitPrompt`; `sessionStart` | `{"continue":true,"additional_context":"<note>"}` and a newline; on `sessionStart`, `{"additional_context":"<note>"}` with the session-start first line. Every character past ASCII is escaped as `\uXXXX`, so the shell Cursor runs hooks through (PowerShell on Windows) can't re-encode it | `agent_id` (a subagent); no workspace folder (above); no pin; the deadline; never `"continue": false` (that blocks the prompt), never `user_message`, never on any other event |
| OpenCode | the generated plugin's `chat.message` (plugin v2), which spawns `rex ingest --source opencode` with `{"type":"context","cwd":"<its directory>","sessionID":"…"}` on stdin | one JSON object and a newline, `{"note":…,"pin":…,"pin_plan":…}`: the note, and the task's id and the plan's repo-relative path as the file spells them (no `pin` for a new plan's line; neither where they would take the reply past 8 KB); the request is never recorded in the feed. The plugin waits for it **at most 1.5 s**, reads at most 8 KB (more is dropped whole), catches everything, and pushes one `{id, messageID, sessionID, type: "text", text: <the note, trimmed>, synthetic: true}` part onto the prompt's own `parts` (never a new array), its id in OpenCode's `prt_` shape sorting after the prompt's own parts. OpenCode sends synthetic text to the model and shows the person none of it | a child session (a subagent's); a prompt with no text of the person's (all synthetic or ignored: a background task's result); no pin; the deadline; empty stdout, a non-zero exit, a reply over 8 KB or not one JSON object, no `rex`, a throw or the 1.5 s timeout (the prompt goes on without the note); never on any other payload |

Only an explicit `--source claude`, `--source codex` or `--source cursor` hook, or an explicit `--source opencode` context request, prints (the reply's shape is the harness's; `auto` never does). Claude Code's handler and Codex's `hooks.json` are byte for byte what they were, so Codex's trust hash holds; `--install claude`, `--install codex`, `--install cursor` and `--install opencode` say the hook (OpenCode's plugin) adds the pin. Cursor's `beforeSubmitPrompt` reply field is undocumented: recent builds add it to that prompt (seen in Cursor 3.20 and its CLI 2026.10), and an older build ignores it harmlessly (3.2 did). The reply keeps `"continue": true`, the one field Cursor's docs give that reply without marking it optional, and the shape every build seen adding the context was sent. Cursor merges `deny` over `allow` and joins messages, but for other fields says only that the last response wins, user hooks last; whether `continue` is one of them is undocumented and unchecked. So Rex's `sessionStart` and `beforeSubmitPrompt` entries go first in their lists in `~/.cursor/hooks.json` (an install adds them there and moves an older one there, keeping fields the person added), and the person's own hooks in that file answer after Rex's. A prompt-blocking hook (a secret scanner, say) in an Enterprise, Team or project `hooks.json` still answers before every user hook, so if `continue` is last-wins, Rex's `true` could undo its `"continue": false`: such a hook must not count on running after Rex's (README and SECURITY say so). The same rule may keep only one hook's `additional_context` per event, so Rex's note can replace another hook's (an organisation's `sessionStart` context, say) or be replaced by one that answers later (the person's own, or a plugin's, which come after user hooks). Cursor's documented floor is `sessionStart`, newly registered: Cursor adds that note to the chat's context once, as of the chat's start, so it can't follow a pin that moves mid-chat. `rex ingest --install cursor` (and the install script) splices the `sessionStart` entry into `~/.cursor/hooks.json` like the others (byte-preserving, a `.bak`, an atomic write, a user's own hooks kept, a file that doesn't parse refused); run it again to add it to an older install, and `--uninstall cursor` takes it out with the rest. `sessionStart` records nothing in the feed. OpenCode reads no hook's stdout; the plugin's `chat.message` is the one place it waits. OpenCode runs that hook before it saves the prompt and awaits it with no timeout of its own, and a throw there fails the prompt, so the plugin bounds and catches it. When it added the note, it sends the reply's `pin` and `pin_plan` with the prompt's event, never a task id or path read back from the note's wording (a task id may hold ` · `, and the note cleans and cuts what it names). The plugin's first line carries its version (`// generated by rex · plugin v2 — safe to delete`); one from before (no version) still feeds the feed but never asks for the note (`Harness.Old` tells it apart; feed health doesn't show it yet). `rex ingest --install opencode` replaces it (an older plugin Rex wrote, never a newer Rex's, which it keeps and says so, or a `rex.js` it didn't), then restart OpenCode: it loads plugins only when it starts, with no trust step. OpenCode's V2 CLI runs no V1 plugin, so neither the feed nor the note works there.

**Safety rails.**
- From a hook, `rex ingest` always exits 0, a panic included (recovered, with no output), and so does a harness that closed stdout before the note (SIGPIPE is ignored, so the note is lost, not the exit code); it never exits 2.
- It writes stdout once, the whole reply at the end, so nothing can leave half a JSON object, and only on the events above.
- It only reads plans and `state.json`; it writes only the feed, as before (`0600`, `~/.rex` `0700`).
- Main agent only: `agent_id`, a Claude payload carrying `cursor_version`, and an OpenCode child session get nothing.
- OpenCode's plugin waits for the note at most 1.5 s, only in `chat.message`, and catches everything, so a slow or broken `rex` costs a prompt at most 1.5 s and never fails it.
- When it prints, the `user` event it records in the same run carries `meta.pin` (the task's id; none for a new plan's line) and `meta.pin_plan` (the plan's repo-relative path), and the feed's dedupe never folds it into the same words just before it when the pin differs. OpenCode's request records nothing; the prompt's event, which the plugin sends right after with the reply's `pin` and `pin_plan` when it added the note, is recorded with those two fields as they came, with no second lookup (an explicit `--source opencode` only), so an edit in between changes nothing. Nothing else in the feed is new. Rex shows nothing of it yet; the note's text is never shown.

---

## 8. The agent marks done

Rex has no completion form. When the agent finishes the task you asked for, the skill has it fill `accomplished` (what actually happened, a retrospective rather than a copy of `does`), `modified` (paths it actually changed), `verified_how` (how it actually checked) and `completed_notes`, then set the task's `status` to `done` (in one save if it can), then set the plan's own `status` to `done` once every leaf is `done` or `skipped`. Then it stops and reports: each task is one PR.

What Rex shows when the file marks the pin done:
- The notice row: `✔ card-highlight done · next up load-outline` for 4 s (`· now load-outline` when the same save set that task `active`; `· the plan is done` once every leaf is `done` or `skipped`, even before the agent's next save sets the plan's own `status`; else `· nothing else is unblocked`), while row 1 says `nothing pinned · next up load-outline · Outline view`. Narrow, the done id clips from the left so the next one stays (§4): `✔ …ghlight done · next up load-outline` at 38 cells. Agents finish in several saves, so a later save that moves no task's status and adds or removes none (only the completion fields, only the plan's own status) keeps this toast up until it expires; a save that moves a task is news of its own.
- AGENT: one line per save, `rex › plan · card-highlight → done`, then e.g. `rex › plan · edited` or `rex › plan · plan → done`.
- The task list (§6): `✔` on the task, its row muted; the tag moves on to what is next (`next up`, or `pinned` when the same save started it); the cursor stays where you left it (§1).
- Its details: `✔ Done · what your agent wrote when it finished it`, then what actually happened (`Did`, `Changed`, `Checked`, `Notes`: `accomplished`, `modified`, `verified_how`, `completed_notes`), then what it set out to do, muted (`Planned`, its `does`; `Description`, `Verify`, `Files`, `Branch`). The next task's details name it under `Builds on`.

---

## 9. The plan file is the hand-off

There is no send-off or planning brief, no clipboard, and nothing to paste. Before each prompt Claude Code, Codex, Cursor and OpenCode get the pinned task as a note (Cursor: on builds that read it, and when each chat starts; OpenCode: from plugin v2, §7a), but the file is the hand-off: a new chat continues the work when you ask your agent to continue the plan ("continue rex/plans/cuj/sendoff-demo.rex.json", or just "continue the plan"); the `rex-plan` skill tells it how: read the plan file, find the current task (the `active` leaf, else the first ready one), work only on it, re-read the file every turn, and write its status and retrospective back. Without a named plan it takes the one a Rex note names, else the one it worked in this conversation, else the plan whose own `status` is `active`. Everything a send-off used to carry (goal, scope, tasks, what was done, where a stacked PR goes) is in the JSON, so nothing can drift from it, and Rex copies nothing.

**The send-off, kept in the plan.** What a send-off told a fresh chat is one enter away, read from that same file: the pinned task's details (what it does and needs, what it waits on, how to verify it, its files, the done task it builds on, what is out of scope, its branch and the plan's goal, §6), which are the fields of the pin note the agent gets (§7a), and a done task's details (what it accomplished). They show what the new chat will read; they are never copied or pasted.

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

**Asking for another task.** Asking the agent to start a task ("start recovery") is how the pin moves, so a prompt that names another task is held until its turn ends (an `idle` or final reply, or the next prompt). An edit of a file listed for that task waits with the prompt, since the agent may edit before it saves the plan that switches (the `rex-plan` skill has it save first). If the agent makes that task the pin first, it was the switch you asked for: nothing shows on the notice row or AGENT. If the turn ends with the pin unchanged, the prompt and those edits are recorded as soft drift then; if the pin moved to a third task, they are judged against that one.

**A batch that writes the plan.** When one batch of events both saves the open plan and edits files (parallel tool calls, a patch touching both), Rex re-reads the plan at the save: edits reported before it are judged against the plan they were made under, edits after it against what it wrote, so neither the finished task's last edit nor the new pin's first one is drift. A patch that touches the plan and code together counts each file at the lesser of the two. This holds for one save of the open plan per poll: with several in one poll (a catch-up after a pause), Rex can only read the last, so edits between them are judged against its pin.

**Display.**
- **The notice row** (PLAN only) shows the last hard drift not yet resolved: `off pin · README.md · outside card-highlight files`, `off pin` in the pin's yellow (it is off the pin, never an error's red), under a new-plan offer and over read-only (§4). It goes once you look at AGENT, which lists it, and drift that happens while AGENT is on screen raises none. Soft drift is AGENT's only. When the plan changes (it was widened), the notice is judged again.
- **AGENT** gets one drift block per pin, updated in place (§10): `rex › drift · <pin>`, then a row per path or prompt, hard ones in the pin's yellow and soft ones muted. Each row is one row laid out like a failed tool call: when it doesn't fit, the path or prompt clips first and `· <why>` keeps at least half the row; a path clips from the left, so its file name stays (`…/session_handler_impl.go`, or the name alone, or, when even the name doesn't fit, `…` and its end: `…_impl.go`), and a prompt from the right:

```
rex › drift · card-highlight
  hard README.md · outside card-highlight files
  soft internal/tui/live_view.go · next to plans.go
```

- Each path or prompt is listed once per pin.
- Changing the pin (or opening another plan) clears the notice and starts a new block, even when the pin comes back to a task that had one. An on-pin edit of the same file clears a hard notice, though not from the block.
- Your options are yours: ask the agent to get back on the task, to pin another one, or to widen the plan (it edits the JSON; Rex reloads it), or start a fresh chat that continues the plan (§9).

---

## 10. Agent

The AGENT tab is what the agent is doing right now, as it happens. Its title, `Agent`, says `live`, or `↓ 12 newer` while you are scrolled up (the rows below the view), or `no events yet`; row 1 is the turn (`thinking`, `running`, `idle`, `waiting on you`, `turn error`) and the pin the agent gets (`pinned card-highlight`, §4).

The stream is bottom-anchored, newest at the bottom. Each block is named by its first characters (`thinking`, `$`, `✗`, `reply`, `[ASK]`, `rex ›`, the right-aligned prompt), so it reads the same without color, and **only two blocks have a rule**: a reply (`reply ───`) and the warm-start line (`── before rex started ──`).

| Block | Looks like | Rows | Spacing |
|---|---|---|---|
| prompt | your prompt in a box at most half the width, right-aligned, its text left-aligned inside (`text`) | ≤ 6, then `(+N more rows)` | a blank row before and after |
| thought run, settled | `thinking ×N  <the latest thought, clipped>`; one thought: `thinking  <text>` (muted, the label bold) | 1 | packed |
| thought run, live | the same first row, then the rest of the latest thought, indented 2 | 2 (1 under 12 rows) | packed |
| command run | `$ <cmd>` per command, clipped (muted); over 5: `$ … +N earlier` (muted), then the last 4, so the fold always hides at least 2 | ≤ 5 | packed |
| failed tool call (`error` with a tool) | `✗ <cmd or path> · <reason>` (red); when it doesn't fit, the command or path clips first and the reason keeps at least half the row; a path clips from the left, at a `/` or the OS's own separator (`\` on Windows, which a path outside the repo and home keeps), so its file name stays (`✗ …/view.go · permission denied`; when even the name doesn't fit, `…` and its end) | 1 | packed |
| turn error | `✗ error · <text>` (red), later rows indented 2 | ≤ 3 | packed |
| reply | `reply ───` across the row (the label bold), then the Markdown indented 2: headings bold, list items (`• `, `1. `) hanging under their text, code as muted `│ ` lines | all | a blank row before |
| ask | `[ASK] <question>`, a bold pill, wrapped under the question | all | a blank row before |
| Rex: plan | `rex › plan · card-highlight → active`, `rex › plan · following <name>` (muted, `rex ›` bold) | 1 | packed |
| Rex: drift | `rex › drift · <pin>`, then `  hard\|soft <path or prompt> · <why>`, each row fitted like a failed tool call, one block per pin (§9a) | 1 + a row each | packed |
| warm start | `── before rex started ──` (muted) | 1 | no blank row after |

Mockup at 38×24 (a 38-cell text column), the turn over, an edit next to the pin's files soft drift in its block:

```
Agent             live   plan · agent
idle            pinned card-highlight


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

↑↓ scroll · tab plan · ? help
```

Mid-turn at 38×12, the newest block a live thought run (the next command settles it to one row):

```
Agent             live   plan · agent
thinking        pinned card-highlight

rex › plan · card-highlight → active
thinking ×4  Looking at the plans vie…
$ Edit internal/tui/plans.go
$ Edit internal/tui/live.go
$ go test ./internal/tui
thinking ×3  The bar is drawn per row,
  so the fill has to start at column…

↑↓ scroll · tab plan · ? help
```

- **Live and settled.** A thought run is live while it is the newest block and the turn is in flight (`thinking`, or `running` after a tool call), scrolled or not. A drawn block under it (a command, a `✗`, an `[ASK]`, a reply) or the turn's end (even one that draws nothing, such as Cursor's `stop`) settles it to one row. A tool call that draws nothing (a file look) never settles it, and scrolling never changes the layout, so one ↑ moves the view one row and every row can be reached.
- **Folding is display only.** Only each run's latest thought shows, and only the last 4 of a run of more than 5 commands. The feed (`~/.rex/session.jsonl`) keeps every event, and Help says so. No key unfolds a run.
- **Narrow and short.** Under 30 text cells a thought run is only `thinking ×N`. Under 12 rows there are no blank rows at all, not between blocks nor inside a reply or a prompt.
- **Scrolled.** Scrolled up, the view keeps the lines it shows: new rows arrive below it and row 0 counts the rows below the view (`↓ N newer`), and a thought or command that joins a folded run adds none (but the second row a live run takes once its latest thought no longer fits on one), nor does a one-row block that settles the live run (it takes the row the run gives back); a run that settles under the view gives its row back without moving it; and the cap of 400 events trims the oldest ones above it without moving it. A scrolled view stays scrolled: at `1↑`, where the live run's first row is the view's last, a turn's end moves the view the one row the run gives back. End (or Ctrl+E) is live again.
- **Hidden.** File-look commands and result echoes stay hidden, and no file body is ever shown: a write is its path.
- **What reaches PLAN.** Only an ask or a turn's own error, as a notice on PLAN: an ask until the turn goes on past it (answered in the agent itself), a turn's error until the next prompt, either until you look at AGENT; never a reply, a thought, a command or a failed tool call (§4).
- **Rex's own lines** (`rex ›` and the warm-start line) exist only in this process and are never appended to the feed. There are no notes: AGENT has no composer (§2).
- **Prompt boxes.** User prompt boxes are right-aligned with left-aligned text. They are capped at 6 rows, followed by `(+N more rows)`.
- **Text cleanup.** Agent text is cleaned before layout:
  - Control characters and escape/OSC sequences are removed, so agent output cannot recolor the screen, retitle the window or write the clipboard.
  - Tabs are expanded.
- **Scope.** Paths in tool lines are shown relative to the repo; replies, thoughts and prompts are shown as written. Where an event came from (its working directory or workspace roots) decides whose it is, so another repo's agent reading a file here stays hidden. Events from other repos' agents (same machine, same feed) are hidden, and the count appears under Help. An event with no working directory or roots is judged by the file it names; with nothing to go on at all, it is shown.
- **Starting mid-turn.** If the newest feed event is under 2 minutes old, the turn state is restored, so the dino is already typing. The last ~20 events from this repo are drawn under a muted `── before rex started ──` rule, laid out like the live stream (runs fold, replies get their rule), with no blank row under the rule.
- **Feed health** is the empty tab: row 0 says `no events yet`, row 1 is its lead, `Rex shows your agent's work here as its hooks report it.` (`Rex shows your agent's work here.` where that doesn't fit), and its body a `FEED` header (H2, §5) and the block under it, so the page never starts mid-sentence. The empty tab is a page, not a stream: it starts at the top and, when it runs past a short screen, scrolls from there as Wait does (§3): ↑↓, PgUp/PgDn and Home/End move it, `↑ more` / `↓ more` say what is past the screen, and only then does its footer offer `↑↓ scroll` (§4). Help shows the same block. The wiring is re-read every few seconds while it is on screen, so an install in another terminal shows up, and the last event follows the feed:

```
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

The `skill` row says how the `rex-plan` skill stands where agents read it: `rex-plan current`, `missing · rex ingest --install-skill`, or `out of date · rex ingest --install-skill` (`… --project` when the old copy is the repo's). A copy is judged by the skill version in its generated marker (`skill v3`), never by its bytes: one with an older version, or none, is out of date; one of this version (a CRLF checkout of a committed `--project` copy too) or a newer Rex's is current, and `--install-skill` never replaces a newer Rex's copy (it says so and succeeds). Start usually lands on a plan, not the Wait screen, so this is where an upgrader sees it.

- **Installs are opt-in.** Rex never installs a hook or a skill on its own. Feed health shows the command, and you run it. The install script wires Cursor and prints the opt-in commands for the other harnesses it finds. Each one is `rex ingest --install <harness>`; `--install-cursor-hook` is an alias for Cursor's. Running an install again brings Rex's entries up to date and keeps everything else (that is how an older Cursor install gains `sessionStart`, and an older OpenCode plugin becomes plugin v2, which hands over the pin, §7a). `rex ingest --install-skill` puts the `rex-plan` skill where all four agents read it: `~/.claude/skills/rex-plan` (or `$CLAUDE_CONFIG_DIR/skills`) for Claude Code and `~/.agents/skills/rex-plan` for Codex; Cursor and OpenCode read both (and may list it twice: the copies are identical). With `$CLAUDE_CONFIG_DIR` set, Rex's copy in `~/.claude/skills`, which Cursor and OpenCode still read, is updated too, and `--uninstall-skill` removes it with the others; a `SKILL.md` there that Rex didn't write is never touched. `--project` puts both copies in the repo instead.

---

## 11. Start, attach and the plan list

Opening a plan is not a step on the happy path: the agent writes the plan, and Rex opens it.

**Landing.** Start never lands on the plan list. It opens, in this order:
1. the plan last opened in this repo, from `~/.rex/state.json`, if that file still exists and is one of this repo's plans (the same jail as attach, below);
2. else the newest `*.rex.json` under `rex/plans/**` by modification time (only at start, or while no plan is open; see the fallback below);
3. else the **Wait** screen: PLAN with no plan, under the title `Plans › Waiting for a plan`, row 1 the plan list's notice (outside git, an old `rex-plans/`), else `Rex opens the plan your agent writes.` (§4). It has no composer (§2) and nothing on it is selectable. Its body, in order:
   1. the headline, verbatim and bold: `no plan yet · tell your agent to make one`;
   2. `HOW IT GOES`: `1  you ask your agent to plan the work`, `2  it writes rex/plans/<name>.rex.json` (outside git too, where the notice above names the folder), `3  Rex opens it here and follows it`. A path too long for its row starts a row of its own and breaks after a `/`, never inside a name;
   3. the keys: `esc  open a plan that already exists`, `tab  watch the agent`;
   4. while the `rex-plan` skill is installed nowhere Rex knows (the user's or this repo's `.claude/skills` or `.agents/skills`, or `$CLAUDE_CONFIG_DIR/skills`), a muted hint with the command in text: `agents learn plans from the rex-plan skill · rex ingest --install-skill` when it fits on one line, else a lead over the command: `agents learn the plan format from the rex-plan skill:` where it fits a row, `agents learn plans from the rex-plan skill:` where only that does, and narrower the first one wrapped, never leaving a word alone above the command. The command is never clipped: it wraps whole. When a copy there is Rex's but an older Rex wrote it (an older skill version in its marker, or none: it still teaches that Rex's workflow), it is `rex-plan skill is out of date · rex ingest --install-skill`, or `… --install-skill --project` when the old copy is the repo's (the user's copies are judged first). With `$CLAUDE_CONFIG_DIR` set, Rex's copy left in `~/.claude/skills` counts like any of the user's: `--install-skill` updates it.

   When rows run short, these go in order: the blank rows, the key rows, `HOW IT GOES`, and the long hint, for the short one (`teach it: rex ingest --install-skill`, or `update: rex ingest --install-skill`, the command under the word where it doesn't fit beside it). The headline and the short hint never go; what still doesn't fit scrolls with ↑↓, pages and Home/End. With plans off (`rex/ is a file here · plans off`) it says only that and how to turn them on: nothing the agent writes can open until the file moves. AGENT works as always; Esc or ← is the plan list, where `+ New plan` stays as an escape hatch, and while no plan is open Esc or ← from there comes back.

```
… › Waiting for a plan   plan · agent
Rex opens the plan your agent writes.

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




esc plans · tab agent · ? help
```

Either plan lands on its task list and is **following**, and the notice row says which file opened (`Opened <title> · <path>`, §6); so does a plan the cold-start scan finds, which no write brought. The plan list's notice, which start no longer shows, is a toast on the notice row on the first frames instead (§4). An upgrader never sees the Wait screen's skill line, so when the skill is out of date that line is the toast instead (it wins over the notice, which the plan list still shows), and Help's feed block keeps a `skill` row (§10, §13). A missing skill is no toast: the Wait screen and Help say so.

**Following and latched.** Row 0's meta says `following` while Rex follows; latched shows no word (§4).
- *Following* (unlatched): Rex opens the plan file the agent writes. Every start is following.
- *Latched*: you picked another plan in the plan list (a row under `RECENT` or `FOLDERS`, a match, a Browse row, or one you created there). That is the only way to latch: Rex has no other action on a plan. enter on the open plan, under `OPEN` or on any row that names it, picks nothing new and keeps a followed plan followed. Rex keeps showing a latched plan; the agent's write of another plan only puts `Your agent wrote <title> (<path>) · o opens it` on the notice row, in both places (§4), and `o` on the task list, its details or About, AGENT or the plan list's Home opens it and follows again (§3). `o` takes it only while the notice row shows it (the plan file's error, your agent's question or a toast cover it); hard drift ranks under it, and while it stands nothing is judged against the latched pin (§9a). enter never takes it. Taking it opens a plan, so it also waits for what holds an attach (below): while the plan list's composer has text or a Name prompt is open, `o` is a letter and the notice drops `· o opens it`. Opening a plan clears the offer. A write held while you pick or create a plan in the plan list becomes the offer, neither opened nor lost, unless it is the plan you picked.

**Attach.** Rex follows a hook event, never a guess. In a batch of events, after they are judged for drift, the last one that passes all of these is opened:
- a completed file edit (a Write, Edit or afterFileEdit result, a Codex or OpenCode `apply_patch`), not a denied or failed write, never a read or a shell command;
- from this repo's agent (§10, Scope);
- of an existing regular `*.rex.json` under this repo's `rex/plans/`, once the path is cleaned and its symlinks resolved: a `..` counts by where it leads, and a link out of `rex/plans/` leads nowhere (a relative path is taken from the event's working directory);
- not the plan already open, judged by the file rather than its spelling (another case on macOS or Windows, a link inside `rex/plans/`). A write of the open plan only reloads it (§6) and never hides another plan written in the same batch or patch: that one is still opened or offered.

Following, it opens the plan's task list (as opening it in the plan list would: §6, §7), and the mascot, where it shows, glances once (§12). The notice row says `Your agent wrote <title> · opened <path>` (seen from AGENT, the toast waits for PLAN, §4), and AGENT gets one muted line, `rex › plan · following <name>`, so the move stays on record (Rex's own line, never the feed). The place never changes. Text in the plan list's composer or a Name prompt (you are finding or making a plan), in either place, holds the attach: the notice row says `Your agent wrote <title> (<path>) · opens when the line is clear` (`· opens when the prompt closes` while a Name prompt is open, §4), and the plan opens once they clear (Esc, an emptied draft, the prompt cancelled). Nothing else holds it: the Wait screen, the outline, the details, AGENT and Help have no composer, so letters typed there leave nothing behind. A newer write replaces a held one; a write of the open plan drops it (the agent is back on that plan); picking or creating a plan in the plan list makes it the offer (above), unless it is the plan you picked. Keys read together with a batch of events are applied first, so a key acts on the screen you saw and a find typed with it holds the attach. The plan's first write may be a stub; the outline fills from the 500 ms reload, and the cursor moves to the pin once tasks appear (§1). A plan that doesn't parse when attach opens it shows its error at once, as any first open does; the two-poll debounce is for a plan already open (§6).

**Fallback.** While no plan is open, Rex also looks for a plan file once a poll (an editor save, or a write whose hook was missed) and opens the newest. Once a plan is open, attached or latched, Rex never scans the folder to change the screen: only a write is followed, and newest-by-mtime is never used again.

The plan list (`Plans`, with the repo and how many plans there are as its meta, §4) is where Esc from the task list and from the Wait screen lands. **Each plan is on it once**, by file, not by spelling: the open plan written in another case (macOS, Windows) is still its one row under `OPEN`, named as the folder names it, and a link inside `rex/plans/` (a symlink or a hard link) to a plan already listed is left out of the groups and the counts, unless the plan was opened by that name (a find still matches a link by its name). Plans in hidden folders (`.archive/`) are left out, as Browse leaves the folder out, except the open plan, which is under `OPEN` wherever it lives; a find still matches them, and start can still land on one as the newest plan. The page's count, a folder's `1 plan` and Browse's count all count this one set, so every plan counted is on the list or in a folder on it. Three groups, each under an H2 (§5):

| Group | H2 meta | Rows |
|---|---|---|
| `OPEN` | `following` / `latched` | The open plan; absent while none is open. Esc from the task list lands here, and enter on it keeps a followed plan followed. |
| `RECENT` | – | Up to 5 other plans: first the ones opened earlier in this run, the latest first (memory only: `state.json` keeps just the last plan), then the newest by modification time. Named by their path under `rex/plans/` (`ux/redesign`), all of it in the row's role. |
| `FOLDERS` | `rex/plans/` | The folders right under `rex/plans/` (`cuj/`, with `1 plan`: the plans in it and below), then the root plans not shown above, by name, then `+ New plan` and `+ New folder`. |

```
Plans    rex · 4 plans   plan · agent
Pick up where you left off, or start…

OPEN ─────────────────────── following
❯ cuj/sendoff-demo                 1/3
    Send-off demo · active
    goal    A teammate opens this pla…
    pinned  ▸ card-highlight  Selecte…
    then    · load-outline
    done    ✔ plans-ui  Home cards, f…

RECENT ───────────────────────────────
  rex/v1-sendoff-loop          ✔ 13/13
  auth-flow                        0/4
  ux/redesign                      2/6

FOLDERS ─────────────────── rex/plans/
  cuj/                          1 plan
  ↓ more
╭────────────────────────────────────╮
│ / find or create a plan            │
╰────────────────────────────────────╯

enter open · tab agent · ? help
```

- **Rows.** Under 60 cells a row is the name with its meta on the right; from 60 cells the name is in a 22-cell column, then the plan's title, muted (`cuj/sendoff-demo      Send-off demo … 1/3`); a plan with no title (a new stub has only an id) leaves it blank. The meta is the plan's progress (`1/3`), `✔ 13/13` once every leaf is done or skipped (the whole row muted, the `✔` green), `no tasks`, or `can't read` (red); at 7 rows, where no `OPEN` H2 says so, the open plan's reads `open · 1/3`, as in a find and in Browse. Folders (`cuj/`, `../`) and actions (`+ New plan`) are `text`, like the names; none has brackets.
- **Cursor and spacing.** The cursor starts on the open plan, else the newest plan, else `+ New plan`, and selection follows the plan, not the row (§1). From 16 rows a blank row goes above each H2 where the selected plan keeps its lines with it; at 7 rows there are no H2s, only rows (§12). A long list scrolls with `↑ more` and `↓ more`: `↑ more` never stands on an H2 or hides only blank rows, and `↓ more` comes right under the last row, never under a blank row or an H2 with nothing under it.
- **The accordion.** The selected plan opens in place under its row, so you see what it is before you open it. Only the plan's own row has `❯` and the selection's purple; the lines under it keep their roles, so the agent's pin keeps its yellow (a computed next up is bold `text`). Its lines, with H3 labels in their 8-cell column (§5):
  1. the title and the plan's status (`Send-off demo · active`), just the status when the plan has no title; from 60 cells, where the title is on the row, the status and when the file changed (`active · changed 2m ago`);
  2. `goal`, on one row;
  3. `pinned` (the agent's active task) or `next up` (the pin worked out, §7), `▸ <id>  <title>`; else `✔ all done` (under the other values) or `no pin · nothing unblocked`;
  4. `then`: the next one or two unfinished leaves after the pin, with their glyphs (§5), and from 60 cells their titles;
  5. `done`: the done task the pin builds on, as its details' `Builds on` names it (§6): its last done dependency, else the plan's last done task, and what it accomplished (`✔ plans-ui  Home cards, folder browser, …`).

  A plan with no tasks says `no tasks yet · the agent adds them`, and one Rex can't read or parse `can't read: <why>`, in red: the cause (`permission denied`, `1:12: …`), never the file's path. When rows run short its lines drop (§12): `then` first, then `goal`, then under 60 cells `done` and the title, from 60 cells the status and `done`; the pin stays. Under 30 cells it is the title and `▸ <id>`, unlabeled. Folders and actions don't open. The list reads each plan once per version of its file, keyed by its path, modification time and size, when the list is built (opening it, a find or filter key, a write the agent reports, a change to the open plan): drawing it and moving the cursor read no files. The open plan is never read for it: its row and accordion, wherever they show, are the plan the task list shows (§6), its last good copy while a save is half-written, and `can't read` only once the notice row shows the error.
- **No plans yet.** `Plans` with `no plans yet`, row 1 `No plans here yet.`, one sentence, `No plans in rex/plans/ yet. Type a name below, or ask your agent to write one.` (at 7 rows it goes), then `+ New plan` (selected) and `+ New folder` (any folders above them). Start shows the Wait screen instead of this; Esc from it lands here, and Esc or ← goes back.
- **Find (typing in the list)** matches across `rex/plans/**` under `Plans › Find`, its meta the number of matches. It is one flat list, folder-qualified and best match first, with no groups:
  - The first match is selected and opens in place; the open plan's meta reads `open · 1/3`.
  - The list always ends with `+ Create 'x'` (unless a plan is named exactly `x`). With no match, that row is selected, and enter creates the plan at once and opens its task list. There is no extra prompt.
- **Browse** (enter or → on a folder; `Plans › cuj/`, then `› sub/` for each folder down; its meta counts the plans in that folder and below, and with a filter the plans under the folders and plans it leaves). There is no Browse root: `FOLDERS` is it.
  - Rows: `../`, the subfolders, the plans (the selected one opens in place; the open one's meta reads `open · 1/3`), other files (muted), a rule, then `+ New plan` and `+ New folder`.
  - The cursor starts on the first folder or plan, and a filter selects its first match.
  - `../`, Esc and ← go up a folder; from a top folder they go back to the plan list, the cursor on that folder.
  - Entering or leaving a folder clears the filter.
  - enter on a non-plan file says `not a .rex.json`.
  - With nothing matching, it shows `no match for 'x' in rex/plans/sub`.
- **Name prompt** (enter on `+ New plan` or `+ New folder`; `Plans › New plan`, from Browse `Plans › cuj/ › New plan`, its meta the folder it creates in): `Name the plan, then enter.` and `a/b makes plan b in folder a.`, then where it goes, updated as you type (`→ rex/plans/auth-flow.rex.json`, `→ rex/plans/<name>.rex.json` while empty, `→ rex/plans/team/<name>.rex.json` while the name ends in a folder, `team/`), then `never overwrites a file`. It creates in `rex/plans/` from the list and in the folder being browsed from Browse, where the preview says. A name that would climb out of `rex/plans/` says so before enter. Short of rows, the blank row goes, then the `a/b` line, the overwrite line and the first line; where it goes stays.
  - `a/b` creates the plan inside folder `a`.
  - Duplicates warn and never overwrite.
  - A name the filesystem refuses says why, with the cause alone (`file name too long`, `permission denied`), never the full path it would have had.
  - A new plan is a valid v1 stub, not `{}`.
- **Errors** are on the notice row (`✗ …`, §4) and clear on the next key.
- **Starting Rex never deletes anything, and never creates a plan file or `rex/plans/`.** The plan start opens is recorded in `~/.rex/state.json` (§7), which may create `~/.rex`. Following or showing a plan never writes the plan file; the only plan Rex ever writes is the new empty stub you create here (never over an existing file).
  - A file named `rex` at the repo root shows `rex/ is a file here · plans off` instead of being removed.
  - Outside a git repo, the plan list says `not in a git repo · plans go to <cwd>/rex/plans`.
  - These notices are row 1 on every plan list screen and on Wait (§4), never a line of the list.
  - Plans left in an old `rex-plans/` folder are reported (`rex-plans/ has 2 old plan(s) · move them into rex/plans`), never moved.
  - `rex/plans/` is created on the first new plan or folder.

---

## 12. Layout tiers

| Terminal | Layout |
|---|---|
| ≥ 110 cols and ≥ 16 rows | mascot (40) + gutter (2) + text column (68 cells at 110 columns); the mascot is cropped from the bottom under 20 rows |
| < 110 cols or < 16 rows | **text only**: no mascot; the text column starts at column 0 and uses the full width (all 80 cells at 80 columns), up to 100 cells |
| text column > 100 cols | the text column stays 100 cells; the rest of each row is empty (§4) |
| text column < 50 cols | a page's labels (task details, About) sit above their values (§6) |
| text column < 40 cols | a task row is its id and its tag (§6) |
| text column < 30 cols | row 0 has no places' label (§4); an AGENT thought run is only `thinking ×N`, without its latest thought (§10); the plan list's accordion is the title and `▸ <id>`, unlabeled (§11) |
| text column < 24 cols | `widen to 24 columns` (`widen to 24 cols` under 19 columns), with no wordmark; keys other than Ctrl+C and Ctrl+L are ignored |
| < 16 rows | the plan list's composer is one borderless row; no blank row goes above an H2 |
| < 12 rows | no blank row 2 and no notice row: a notice takes row 1 while it shows (§4). The task list has no group labels and no blank rows, and its `↑ N more` (a page's too) takes a row of its own (§6). At 11 rows the task list, its pages and the plan list's accordion are laid out as at 12 rows, so they never shrink as the terminal grows (below). AGENT has no blank rows, and a live thought run takes 1 row instead of 2 (§10) |
| < 7 rows | the plan list's composer is hidden: printable keys and pastes are ignored there, so nothing is typed blind, and enter never submits its draft. Its footer says only what works, whatever row is selected: `type to find` / `type to filter` go; `enter` stays on an empty draft (it still opens or starts the selected row) and goes while a hidden draft has text and in a Name prompt, which nothing can be typed into (`esc cancel` takes you out); and `? help` goes while a hidden draft has text. Screens without a composer keep their keys and footers; `?` on an empty draft, and on screens without a composer, still opens Help |
| `TERM=dumb`, pipes, CI | the static card (`rex start --static`) |

Body rows between the chrome (§4):

| Terminal rows | PLAN screens / Help / AGENT | plan list (with its composer) |
|---|---|---|
| 24 | 19 | 16 |
| 16 | 11 | 8 |
| 12 | 7 | 6 |
| 11 | 8 (the task list and its pages laid out in 7, as at 12 rows) | 7 (its accordion sized as at 12 rows) |
| 7 | 4 | 3 |

From 12 rows the blank row 2 and the notice row take two rows that an 11-row terminal gives the body, so 11 rows lay the task list, its pages and the plan list's accordion out as 12 do, the row left over blank: a taller terminal never shows less of them. The chrome is never a body line, so the lines that stay put above a list and the selection margin never see it. A selection taller than the rows its list has shows its top, so a plan's own row is never the part cut.

The task list's shape (§6) is one function of its body rows and of whether there are 12 of them: its blank rows between groups go in only where the whole list fits with them, and under 12 rows its labels and blank rows go and `↑ N more` takes a row. The selection never decides either, so moving it never moves the list.

The plan list's shape (§11) is one function of its body rows, which the selection margin reads too:
- its H2s need 5 body rows, so at 7 rows it is only rows: no H2, no blank row, no accordion;
- the accordion takes the rows the rest of the screen leaves (the rest keeps at most 5: the selected row and two either side), up to its 5 lines. On the list that is all of them from 18 rows, 3 at 15 and 16 rows (the title, the pin and `done`; from 60 cells the status in the title's place), the pin alone at 11 and 12 rows, none at 7; a find or a folder with few rows has more. A 15-row terminal sizes it as a 16-row one, where the composer's box takes two rows, and an 11-row one as a 12-row one, where row 2 and the notice row take two, so a taller terminal never shows fewer of its lines;
- the blank rows above each H2 but the first (whose blank is row 2) go in from 16 rows, and only where the accordion keeps its lines with them.

Notes:
- **The mascot.** Its poses don't change with the layout: typing while a turn is in flight, idle otherwise, `/breakdance` once. When a plan opens (start's, an attach, a pick or a create in the plan list, an offer taken with `o`), it **glances once**: the idle grid's glance transform for about a second (14 frame ticks), on the frame clock, over whatever pose plays, whose own frames keep their time under it. A reload of the open plan never glances, nor does an open on a terminal too small to show the mascot, and a breakdance plays whole. No key waits for it.
- **No full clears.** Rex never clears the whole screen on a normal frame. Each changed row is erased from column 1 and then written whole, inside synchronized-output markers. Nothing is erased after a row: after a write to the last column the cursor stays there, and an erase would wipe that cell. The cursor is hidden while painting.
- **Repaints.** Resize, `SIGCONT` and Ctrl+L force a full repaint.
- **Widths.**
  - Text is clipped by **cell width**: East Asian wide characters and emoji count 2, combining marks 0.
  - All East-Asian-ambiguous characters count 1, including `·`, `…`, `↑↓`, `○`, `•`, `›`, `×`, `→` and box drawing; `❯`, `✔` and `✗` are narrow and count 1 too (a test pins them). "Ambiguous characters are wide" terminal settings are not supported.
  - The width table is a small range list written by hand in `internal/tui`. The standard library has no width data, and no new module is added for it.

---

## 13. Help overlay (`?`)

`?` opens it at once from any screen (§2); `/help` then enter does too in the plan list. Its title is the crumbs of the page under it, then `Help` (`Send-off demo › Help`), row 1 `Keys for the task list, then everywhere.`, and it keeps its place across Tab. It keeps `THIS PAGE` and its scroll while it is open: a plan the agent writes meanwhile opens under it (§11) without changing them, and Esc shows it. The crumbs follow the page under Help, so they always name where Esc goes (§4). Its sections, each under an H2:
1. `THIS PAGE`, with the page's name as meta (`tasks`, `no tasks`, `can't read`, `about`, `task`, `plans`, `folder`, `new plan`, `new folder`, `waiting`, `agent`): the keys of the page it was opened over. Two pages have a note under the keys, with no key beside it: on `agent`, that folding is display only: the feed keeps every thought (§10); on `plans`, that the selected plan opens in place, with its goal, pin, what's next and what's done (§11).
2. `EVERYWHERE`: `tab` (switch plan / agent), `?`, `ctrl+l`, `ctrl+c ×2`.
3. `MARKS`: the task list's glyphs and tags of §5 (`✔ – !`, and no mark for a task still to do; `pinned` the task your agent set active, which it gets with every prompt, in the pin's yellow; `next up` nothing pinned: the task Rex would pin, never sent; `also active` a second active task, and `cycle` a task in a loop, in red; a group's `1/3`), the cursor `❯`, and what `following` / `latched` mean (§11). AGENT's marks are under `AGENT TAB`.
4. `AGENT TAB`: how AGENT names its blocks (§10): `thinking` (a run of N thoughts, `×N`), `$`, `✗`, `[ASK]`, `reply ───`, `rex ›`.
5. `PLAN LIST`: what typing does there (§2), `a/b`, Ctrl+U / Ctrl+W, `?` or `/help`, and `/breakdance`.
6. `REX NEVER`: runs or approves tools, edits a plan (it only creates an empty one you name in the plan list), sends your agent anything but the pinned task before each prompt (or a new plan that has no tasks yet, §7a), or reads transcripts; your agent pins and finishes tasks in the file, and Rex shows it.
7. `FEED`: the feed path and feed health (§10), with the `skill` row (whether the `rex-plan` skill is current, missing or out of date, and the command to fix it), and the hidden-event count.

`THIS PAGE` lists every key its page takes, and only those, not only its footer's (a plan with no list has no enter, → or task to move to: its page scrolls on a short screen): on the task list, About, task details, the plan list and AGENT, `o`, and on the task list `i`, in the same words (`open the plan your agent wrote, while the notice row offers it`); Home and End, with Ctrl+A and Ctrl+E (§3), wherever they move or scroll, Browse and Wait included; PgUp/PgDn on Wait. A test holds each page to the keys that act on it.

The key column is 11 cells wide, keys in bold; under 34 cells, or for a key that fills the column, the key sits above what it does. No key is an icon (`↔`, `⏎`): keys are words.

It has no composer: letters do nothing in it. ↑↓ and pages scroll it, Esc closes it, Tab leaves it open in its place.

---

## 14. Keystroke budget

The full-loop tests check these counts.

| Step | Keys |
|---|---|
| Open the plan you had last time | 0 (start opens it) |
| Create a plan | 0 in Rex: ask the agent to write `rex/plans/<name>.rex.json`; Rex opens its task list |
| Open a plan by name | Esc `name` enter (latches it) |
| Open the plan you had open before, else the newest other plan | Esc ↓ enter (the first under `RECENT`; latches it) |
| Create a plan in Rex | Esc `name` enter via find → `+ Create 'name'`; lands on its task list (latched) |
| Open a plan the agent wrote while latched | `o` (the notice names it) |
| See the pin | 0 (row 1, the pin line) |
| See what the pin hands your agent | enter (the cursor lands on the pin; its details are the note's fields) |
| Watch the agent | Tab |
| Back to the plan | Tab |
| Pin a task | 0 in Rex: ask your agent |
| Mark the pin done | 0 in Rex: ask your agent |
| Continue in a fresh chat | 0 in Rex: ask the new chat to continue the plan (§9) |
| Help | `?` |
| Quit | Ctrl+C Ctrl+C |

---

## 15. Transitions

Each state's title (§4) names it.

| State | Page title | Entered via | Left via |
|---|---|---|---|
| Wait | `Plans › Waiting for a plan` | start with no plan to open · Esc/← from the plan list's Home while no plan is open | an attach (the agent writes a plan, or the fallback finds one) → Task list · Esc/← → plan list Home · `?` · Tab |
| plan list Home | `Plans` (`Plans › Find` while finding) | Esc from Wait, from a top folder in Browse (cursor on that folder) or from the task list (cursor on the plan, under OPEN) | enter on a plan or match (latches another plan; the open one keeps following) · enter/→ on a folder → Browse · enter on `+ Create 'x'` creates and opens its task list · typing (find) · Esc/← → Wait while no plan is open · an attach while following (with an empty find) · `o` on a new-plan offer (with an empty find) · `?` on an empty find · Tab |
| Browse | `Plans › cuj/ › …` | enter/→ on a folder under FOLDERS | enter on a plan → Task list · enter/→ on a folder · `../`/Esc/← up (from a top folder, to the plan list) · an attach while following (with an empty filter) · Tab |
| Name prompt | `Plans › New plan` / `New folder` | `+ New plan` / `+ New folder` rows | enter → Task list (plan) or into the new folder · Esc (nothing written) · Tab (name kept). An attach waits until it closes, and creating a plan makes it the offer |
| Task list | `<title>` | start (the last or newest plan) · an attach · open or create a plan · `o` on a new-plan offer · Esc from task details or About · Tab back | enter/→ → Task details · `i` → About · Esc/← → plan list · an attach of another plan while following · `o` on a new-plan offer · `?` · Tab |
| About | `<title> › About` | `i` on the task list | Esc/← → the task list · an attach of another plan while following · `o` on a new-plan offer · `?` · Tab |
| Task details | `<title> › <id>` | enter/→ on a task | Esc/← → the task list, on that task · an attach of another plan while following · `o` on a new-plan offer · `?` · Tab |
| Help | `<the page under it> › Help` | `?` anywhere (in the plan list, on an empty draft) · `/help` enter in the plan list | Esc · Ctrl+C |
| AGENT | `Agent` | Tab | Tab · `?` · `o` on a new-plan offer (PLAN switches, you stay) |
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
| F17 | med | The plans header drops turn state and path; the open plan is invisible in the library | Header on every screen; Continue card (§4, §11). *Replaced:* the open plan is the plan list's `OPEN` group (F43) | `feature/ux_chrome`, `feature/plan_outline` |
| F18 | med | Home: inert composer, dead cards, sticky errors, Open latest takes ↓↓⏎ | Find on Home; hide dead cards; clear errors; default cursor (§11). *Replaced:* no cards; `OPEN` / `RECENT` / `FOLDERS` groups (F43) | `fix/library_navigation` |
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
| F43 | high | The plan list is confusing and crowded: four cards (Continue, Create, Open, Open latest) over a recents list, so the open plan shows twice and a plan up to three times, Browse repeats the root, and nothing groups or spaces the rows (2026-10-24 feedback) | `OPEN` / `RECENT` / `FOLDERS` under H2s, each plan once, `RECENT` leading with the plans opened earlier in the run; a blank row above each H2 from 16 rows where the accordion keeps its lines; actions without brackets; FOLDERS replaces Browse's root, and `../`, Esc and ← from a top folder go back to the list (§11) | `feature/plan_list_groups` |
| F44 | med | A plan's row says only its progress: what it is for, what is pinned and what got done take opening it (2026-10-24 feedback: details on hover) | The selected plan opens in place: title and status, `goal`, `pinned` / `next up`, `then`, `done`, dropping in a fixed order as rows run short, from a summary cached by path, modification time and size, so moving the cursor reads no files (§11, §12) | `feature/plan_list_groups` |
| F45 | high | The pinned task isn't evident: the pin row, parents, Task detail's title and a rule were all yellow, and the pin and the cursor looked alike (2026-10-24 feedback: border, colour, "Pinned") | A PINNED card in the pin's yellow under the goal, `[PINNED]` / `[NEXT UP]` on its edge; the pinned row is the outline's only yellow row and keeps its yellow on the cursor's fill; parents are bold text; titles and rules are never yellow; done rows show what they accomplished (§5, §6). *Replaced:* the pin's one yellow is its `pinned` tag on the task list, and row 1's pin line names it (F51) | `feature/outline_pin_card` |
| F46 | high | A fresh chat's send-off isn't on screen: what to do, how to check it and what it builds on take opening the task, or the transcript (2026-10-24 feedback: "have the send off in the plan already") | The PINNED card shows it, read from the plan file (`does`, `needs`, `verify`, `files`, the done task it builds on), folding by a collapse ladder that never costs a task row and gives back what fits again; a pin with no fields yet says so inside the box; About's `DONE` lists every done task and what it accomplished. Rex still copies and sends nothing (§6, §9). *Replaced:* the pin's details, one enter away, are the pin note's fields (F51) | `feature/outline_pin_card` |
| F47 | high | Pinning a task does nothing for the agent: it learns the pin only if it re-reads the plan, and loses focus (2026-10-24 feedback: "We must make sure the agent refers to the pinned task requirements so that it doesnt lose focus") | Before each prompt, Claude Code's, Codex's and Cursor's hooks and OpenCode's plugin hand it the explicitly pinned task as a short factual note read from the plan file (or a new plan with no tasks yet), capped at 1,500 bytes, within 750 ms of the hook's start (OpenCode's plugin waits at most 1.5 s for it), and the prompt's event records it; Cursor also gets it when each chat starts; nothing pinned, nothing sent (§7a) | `feature/pin_context`, `feature/pin_context_cursor`, `feature/pin_context_opencode` |
| F48 | high | The UI is loud: yellow meant the pin, live work, commands, news and the brand; cyan, orange and green tinted the cursor, prompts, folders and actions; fills and slate bars sat behind the cursor, the tabs and every page title (2026-10-24 feedback: "Overall we need to tone down the UI as it is too overwhelming") | Quiet roles: yellow is only the pin, purple only the selection (`❯` in the gutter, no fill), green only `✔`, red only an error; titles are bright and bold; `pickbg`, `folder` and `you` retire and nothing is filled (§5) | `feature/quiet_roles` |
| F49 | high | Too many moving parts: row 0 had five elements (`REX`, two pills, a cue, the turn word) and a path, two of them always moving; row 1 had six jobs (the pin, toasts, drift, errors, offers, progress) and changed several times a minute; the page title was a second title row; footers changed as the cursor moved, and enter meant open or switch by what row 1 said (2026-10-24 feedback: "Theres way too many moving parts and it feels very cluttered") | One frame on every screen: row 0 a breadcrumb title with its meta and a quiet `plan · agent` label, row 1 what the page is (the pin line on the outline), one notice row by priority, a muted ` · ` footer fitted by its longest forms; offers taken with `o`, which their notice names (§3, §4) | `feature/quiet_chrome` |
| F50 | high | At 80 columns the mascot took half the width and left the plan 38 cells, so titles, the pin line and the footer clipped on the most common terminal; and nothing on screen moved when a plan opened (2026-10-24 feedback: "At 80 columns the dinosaur takes half the width, which leaves 38 cells for text… show it from about 110 columns. Narrower terminals give the plan the full width.") | The mascot only from 110 columns and 16 rows; narrower or shorter, the text takes the whole width up to 100 cells; no `REX` wordmark, even on the widen screen; where it shows, the mascot glances once when a plan opens, never on a reload (§12) | `feature/mascot_wide` |
| F51 | high | Opening a plan said little that it had opened, then showed too much: a goal, a ten-row PINNED card, a `TASKS` rule and a blurb on every row; and enter on the plan's title or a parent led somewhere unexpected (2026-10-24 feedback: "there isnt much feedback to show that you actually opened a plan and once you do, you're greeted with so much information its very overwhelming… Its very hard to tell what you can and cannot click enter on") | One task list: the pin line on row 1, then a row per task (a glyph, the id, the title, one tag), group labels the cursor skips, the title not selectable; the notice row says which file opened; enter opens a task's details on a page of their own, the pin note's fields; About on `i` (§6) | `feature/task_list` |

Found in the same audit, outside the UI:

| ID | Sev | Problem | Fix | Slice |
|---|---|---|---|---|
| H1 | critical | `rex ingest` exits 2 on a bad flag or source; Claude Code, Cursor and Codex treat exit 2 as **block** | Ingest always exits 0 with empty stdout; stdin and lock deadlines | `fix/ingest_fail_open` |
| H2 | high | The Cursor installer overwrites `hooks.json` it can't parse; writes non-atomically; quotes with Go syntax; registers shell commands twice | Safe merge, `.bak`, atomic write, shell quoting | `fix/cursor_installer` |
| H3 | med | Every hook call reads the whole feed file | Seek-based tail read | `fix/ingest_fail_open` |
| H4 | med | After the feed is truncated, the tail keeps a stale partial line and glues the next event to it, which is then dropped | Clear the pending fragment on truncation | `fix/ingest_fail_open` |
