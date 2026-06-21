# Contributing to NexoraCLI

Thanks for helping improve the Nexora terminal client! It's a Go + Bubble Tea TUI — a pure
*client* that talks to a Nexora / NexoraCloud instance over REST + WebSocket.

## Ground rules

- Be respectful — see [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
- Security issue? **Do not** open a public issue — see [`SECURITY.md`](SECURITY.md).

## Building

There is **no Go toolchain expected on the host** — every Go command runs inside the
`golang:1.23` container via the Makefile:

```bash
make tidy        # go mod tidy
make build       # → bin/nexora
make build-all   # → dist/ for linux-amd64, darwin-arm64, windows-amd64
make vet test fmt
```

The compiled binary runs natively; only the build is containerized.

## Conventions

- **Theming:** reskin in `internal/tui/theme.go` *only* — don't scatter colors across screens.
- **Layout:** one screen per file under `internal/tui/`; the root + shared model live in `app.go`.
- **Streaming:** use the channel + `waitForFrame` re-arm pattern (see `.claude/skills/bubbletea-tui`).
- Never commit `config.toml`, `bin/`, or `dist/` (all gitignored — config holds tokens).

## Submitting changes

1. Branch off `main`.
2. Use [Conventional Commits](https://www.conventionalcommits.org): `feat(tui):`, `fix(api):`, `chore:`.
3. Run `make vet test` before opening the PR.
4. Open a PR describing *what* changed and *why*. Link any related issue.

Smaller, focused PRs merge faster.
