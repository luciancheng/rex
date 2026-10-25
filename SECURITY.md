# Security

## Supported versions

Rex has no tagged releases yet. Security fixes go to the tip of `dev`; once releases exist, to the latest one too.

## Reporting a vulnerability

Please don't open a public issue. Report it privately through GitHub: on [the repository](https://github.com/luciancheng/rex), open **Security → Report a vulnerability** ([direct link](https://github.com/luciancheng/rex/security/advisories/new)). If that form isn't available, open an issue that only asks a maintainer to contact you, with no details, and one will reach out.

Include what you found, how to reproduce it, how you installed Rex, the output of `go version -m` on the `rex` binary (`rex --version` doesn't name the commit), and the OS and agent you used. Please allow a few days for a first reply.

## What is in scope

- the hook and plugin entries `rex ingest --install …` writes into Cursor, Claude Code, Codex and OpenCode configs, and the rex-plan skill files;
- `rex ingest` blocking or steering an agent: exiting non-zero when run from a hook, or writing to stdout anything but the pin note on the prompt hooks docs/ux.md §7a names (plan text that escapes the note's cleaning, its `system-reminder` defusing or its 1,500-byte cap counts);
- `~/.rex/session.jsonl` or `~/.rex/state.json` becoming readable by other users;
- agent or plan text that paints terminal escape sequences, or reaches a shell command Rex suggests;
- `scripts/install.sh` and `scripts/install.ps1`.

Rex makes no network connections itself; issues in the agents it watches belong to their own projects.
