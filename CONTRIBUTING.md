# Contributing

Thanks for helping. Rex is small on purpose, so a few rules keep it that way. The full contract (code conventions, the Go 1.22 floor, the branch and PR workflow, the pre-PR gate and the review pass) is [AGENTS.md](AGENTS.md). It is written for people and coding agents alike.

## Before you start

- **Open or find an issue first.** One branch closes one issue.
- **Rex is watch-only.** It never runs, approves or steers the agent, and the only thing it sends it is the pinned task (or the name of a new plan that has no tasks yet), as a note before each prompt (docs/ux.md §7a); a change that does more won't be merged. AGENTS.md lists the other things Rex doesn't do (new palettes, new dependencies, transcript scraping, …).
- Security issues go through [SECURITY.md](SECURITY.md), not a public issue.

## Making a change

1. Fork, then branch from `dev`, named after the issue (`fix/key_input`, `feature/task_completion`, `docs/ux_spec`).
2. Add `*_test.go` next to the package for new behaviour. Tests use `t.TempDir()` and never touch the real `~` or `~/.rex`.
3. Before you push, at least:
   ```bash
   gofmt -l .        # prints nothing
   go vet ./...
   go test ./...
   ```
   The full pre-PR gate is in AGENTS.md (Workflow). CI runs the tests on Ubuntu, macOS and Windows with Go 1.22 and 1.24, and reviewers run the rest.
4. Never `go build -o rex` at the repo root: `rex/` is the plans folder. Use `go build -o dist/rex ./cmd/rex`.
5. Open the PR against `dev`. The template asks for the problem, what changed, why, how it works, the tests you ran (with output), terminal captures for TUI changes, and anything that changed on purpose.

## Code of conduct

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
