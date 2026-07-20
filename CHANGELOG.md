# Changelog

All notable changes to NexoraCLI. Newest first; one `## <version>` heading per release.
The release CI extracts the section matching the pushed tag as the GitHub Release notes.

## 0.7.2

- Fixed a bug where switching between chats (or a single reconnect) could put the chat into a rapid connect and disconnect loop, so new messages were never answered. Closing the previous chat socket no longer triggers a spurious reconnect of the current one.
- Stream frames and close events from a chat socket that has already been replaced are now ignored, so they can no longer render onto the wrong chat or restart a dead reader.

## 0.7.1

- The chat WebSocket now reconnects with capped exponential backoff (1s, 2s, 4s up to 30s) instead of retrying every second forever, so a persistent connection failure no longer floods the server with reconnect attempts.

## 0.7.0

- New "nexora instance delete <name>" command (aliases rm, remove) removes a saved instance and its tokens from the local config.
- Deleting the active instance reassigns the current selection to another saved one, or clears it when none are left.

## 0.6.0

- New "nexora join" command redeems an organization invite in one step: it creates or signs in your account, pairs this terminal, and saves the instance, with no manual login.
- The install one-liner now takes --join <token> --url <instance> (or the NEXORA_JOIN_TOKEN and NEXORA_URL environment variables), so an admin can share a single copy-paste command that installs the CLI and connects it.
- The installer adds the binary to your shell PATH (.zshrc, .bashrc or .profile) and, when a join token is present, runs the join for you and points you at your instance.

## 0.5.3

- The "thinking" spinner next to the assistant header now actually animates while a turn is streaming, instead of sitting frozen.
- Turn errors are shown as a single plain red line with no icon: a short reason plus a hint, classified for the common cases (invalid API key, rate limited, quota exhausted, access denied, server error). Example: "opencode-zen: invalid API key (401) - update this account's key in Settings > Accounts".

## 0.5.2

- The connection dot in the header no longer gets stuck on red after a one-off failure at startup (for example the server restarting). A successful profile or permissions load now marks the connection healthy, and a periodic heartbeat refreshes it while you are idle.

## 0.5.1

- Failed turns now show a red error card in the chat the moment they happen, instead of leaving the turn blank until you switch chats and back.
- Provider errors are parsed to a short headline plus reason (for example the "Invalid API key" behind a 401) rather than a raw dictionary dumped as if it were the assistant's reply.
- A persisted error message is rendered as the same red card on reload, never folded into a normal assistant bubble.

## 0.5.0

- Respects the per-user limits an org admin sets on the server. The TUI now hides menu tabs and command-palette entries for sections you do not have permission to see, matching the web app.
- Forces the simple interface when your organization has disabled advanced mode for you, and locks the interface toggle in Settings.
- Hides the Settings tab when you are not allowed to open settings.
- Agents, providers, skills, tools, personas and chains already come filtered to what you are assigned, so the pickers only show what you may use.

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
