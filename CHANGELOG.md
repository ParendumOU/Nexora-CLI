# Changelog

All notable changes to NexoraCLI. Newest first; one `## <version>` heading per release.
The release CI extracts the section matching the pushed tag as the GitHub Release notes.

## 0.4.2

- Self-update: `nexora update` downloads the latest release binary for your OS and replaces the running executable in place; `nexora update --check` only reports whether a newer version exists.
- On launch the TUI checks GitHub for a newer release (at most once a day, cached) and shows an "update available" hint in the header when one is out.
- Set NEXORA_NO_UPDATE_CHECK=1 to disable the launch-time check; dev builds never check.

## 0.4.1

- Connect to older instances again: the chat WebSocket now retries with the legacy token query param when a core older than v1.10.0 rejects the Authorization header, so a current CLI still reaches an older server (login worked but chat would not connect before).
- Modern instances are unchanged: the token is still sent only in the Authorization header and never in the URL.
- Release binaries added for linux-arm64, darwin-amd64 and windows-arm64 (fixes install on Intel Macs, ARM Linux and Windows on ARM).

## 0.4.0

- One-liner installers: install.sh (Linux/macOS) and install.ps1 (Windows) download the latest release binary and put it on PATH as nexora.
- Install one-liners documented in the README.
- Release binaries for linux-amd64, darwin-arm64 and windows-amd64 attached to the GitHub release.
- Security: the chat WebSocket auth token is sent in the Authorization header instead of the URL.
- Strip orphan closing think tags from rendered chat content.

## 0.3.0

- Flash / Think / Deep reasoning mode: `ctrl+r` cycles it, a footer chip shows the
  current mode, and it is sent with each turn so the server drives provider-native
  reasoning.
- Synced with the core contract changes: `stream_end` content is treated as
  authoritative (an explicit empty turn no longer resurrects the raw streamed
  buffer), and a still-open `<think>` block renders live as reasoning instead of a
  raw tag.

## 0.2.1

- First public GitHub release.
- Full frontend parity: streaming chat, agents CRUD, providers, knowledge bases, board,
  issues, schedules, marketplace, settings.
- Local tool execution on the CLI host (`--local-exec` / `/local`) with the Local Operator agent.
- Projects detail subtabs + git repo browser, Telegram channels, agent hierarchy tree,
  sub-chat navigation, real-time cross-client sync, instance migration (`nexora migrate`).
- README with install, quick-start, keybindings, and configuration.
