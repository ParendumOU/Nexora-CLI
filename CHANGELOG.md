# Changelog

All notable changes to NexoraCLI. Newest first; one `## <version>` heading per release.
The release CI extracts the section matching the pushed tag as the GitHub Release notes.

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
