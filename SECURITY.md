# Security

## Supported versions

Rex has no tagged releases yet. Security fixes go to the tip of `dev`; once releases exist, to the latest one too.

## Reporting a vulnerability

Please don't open a public issue. Report it privately through GitHub: on [the repository](https://github.com/luciancheng/rex), open **Security → Report a vulnerability** ([direct link](https://github.com/luciancheng/rex/security/advisories/new)). If that form isn't available, open an issue that only asks a maintainer to contact you, with no details, and one will reach out.

Include what you found, how to reproduce it, how you installed Rex, the output of `go version -m` on the `rex` binary (`rex --version` doesn't name the commit), and the OS and agent you used. Please allow a few days for a first reply.

## What is in scope

- the hook and plugin entries `rex ingest --install …` writes into Cursor, Claude Code, Codex and OpenCode configs, and the rex-plan skill files;
- `rex ingest` blocking or steering an agent: exiting non-zero when run from a hook, or writing to stdout anything but the pin note on the prompt and session-start hooks and the OpenCode plugin's request docs/ux.md §7a names, in the shapes it names (for Cursor, a reply that blocks the prompt or shows the person a message counts too, as does plan text that escapes the note's cleaning, its `system-reminder` defusing or its 1,500-byte cap);
- the OpenCode plugin failing a prompt, holding one up past 1.5 s, adding anything to it but the note, or sending the feed more than its allow-listed fields;
- `~/.rex/session.jsonl` or `~/.rex/state.json` becoming readable by other users;
- agent or plan text that paints terminal escape sequences, or reaches a shell command Rex suggests;
- `scripts/install.sh` and `scripts/install.ps1`.

One known limit: Rex's reply to Cursor's `beforeSubmitPrompt` carries `"continue": true` (the one field Cursor's docs give that reply without marking it optional; Rex never sends `false`). Cursor doesn't document how `continue` merges across hooks; for fields other than permissions and messages it says the last response wins, Enterprise, then Team, then project, then user hooks, then plugins' last. Rex's `sessionStart` and `beforeSubmitPrompt` entries go first in `~/.cursor/hooks.json` (`rex ingest --install cursor` moves an older one there), so your own hooks in that file answer after Rex's. But a prompt-blocking Cursor hook, such as a secret scanner, in an Enterprise, Team or project `hooks.json`, or one of yours placed ahead of Rex's entry in `~/.cursor/hooks.json` (a rerun of `rex ingest --install cursor` moves Rex's back to first), must not count on its `"continue": false` outlasting Rex's reply. The same rule may keep only one hook's `additional_context` per event, so Rex's note can replace another hook's (an organisation's `sessionStart` context, say) or be replaced by one that answers later (your own hook, or a plugin's). If you can show Cursor sending a prompt another hook blocked because of Rex's reply, report it as above.

Rex makes no network connections itself; issues in the agents it watches belong to their own projects.
