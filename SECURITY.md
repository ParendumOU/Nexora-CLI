# Security Policy

## Reporting a vulnerability

If you discover a security vulnerability in NexoraCLI, **please do not open a public issue.**

Instead, email **info@parendum.com** with:

- A description of the vulnerability and its impact.
- Steps to reproduce (proof-of-concept if possible).
- The affected version / commit.

We aim to acknowledge reports within **72 hours** and to provide a remediation timeline after
triage. We will credit reporters who wish to be named once a fix is released.

## Supported versions

Security fixes target the latest tagged release on `main`. Please upgrade before reporting.

## Notes on this client

- The config file (`<os-config-dir>/nexora/config.toml`) holds access tokens and is written
  `0600`. Never commit it or share it.
- **Local execution** (`--local-exec` / `/local`) runs shell and file tools on your host. It is
  **off by default**, gated by interactive consent prompts, and the CLI — not the server — is the
  gatekeeper. Only enable it against instances you trust. `--yolo` disables the prompts; use with care.
