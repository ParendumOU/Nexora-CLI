#!/usr/bin/env bash
# NexoraCLI installer — Linux / macOS
#
#   curl -fsSL https://raw.githubusercontent.com/ParendumOU/Nexora-CLI/main/install.sh | bash
#
# Downloads the latest release binary for your platform and puts it on your
# PATH as `nexora`. No runtime dependencies. Safe to re-run (updates in place).
#
# Overrides: NEXORA_CLI_VERSION (e.g. v0.19.0), NEXORA_CLI_BIN_DIR
set -euo pipefail

REPO="ParendumOU/Nexora-CLI"
API="https://api.github.com/repos/$REPO"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[32m ✓ \033[0m%s\n' "$*"; }
fail() { printf '\033[31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || fail "curl is required."

# ── Platform ──────────────────────────────────────────────────────────────────
OS="$(uname -s)"
case "$OS" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *) fail "Unsupported OS: $OS (use install.ps1 on Windows)" ;;
esac
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) fail "Unsupported architecture: $ARCH" ;;
esac

bold ""
bold "  NexoraCLI — terminal client for Nexora"
bold ""

# ── Latest version ────────────────────────────────────────────────────────────
TAG="${NEXORA_CLI_VERSION:-}"
if [ -z "$TAG" ]; then
  info "Looking up the latest release"
  TAG="$(curl -fsSL "$API/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  [ -n "$TAG" ] || fail "Could not determine the latest release. Set NEXORA_CLI_VERSION=vX.Y.Z and re-run."
fi
ok "Version $TAG"

# ── Download ──────────────────────────────────────────────────────────────────
BASE="https://github.com/$REPO/releases/download/$TAG"
TMP="$(mktemp)"
FOUND=""
for ASSET in "nexora-$TAG-$OS-$ARCH" "nexora-$OS-$ARCH"; do
  info "Downloading $ASSET"
  if curl -fsSL -o "$TMP" "$BASE/$ASSET" 2>/dev/null; then
    FOUND="$ASSET"
    break
  fi
done
if [ -z "$FOUND" ]; then
  rm -f "$TMP"
  fail "No prebuilt binary for $OS-$ARCH in release $TAG.
Check https://github.com/$REPO/releases/latest or build from source (see README)."
fi
chmod +x "$TMP"

# ── Install ───────────────────────────────────────────────────────────────────
BIN_DIR="${NEXORA_CLI_BIN_DIR:-}"
if [ -z "$BIN_DIR" ]; then
  if [ -w /usr/local/bin ]; then
    BIN_DIR=/usr/local/bin
  elif command -v sudo >/dev/null 2>&1 && [ -e /dev/tty ]; then
    BIN_DIR=/usr/local/bin
  else
    BIN_DIR="$HOME/.local/bin"
  fi
fi
mkdir -p "$BIN_DIR" 2>/dev/null || true
if [ -w "$BIN_DIR" ]; then
  mv "$TMP" "$BIN_DIR/nexora"
else
  info "Installing to $BIN_DIR (you may be asked for your sudo password)"
  sudo mv "$TMP" "$BIN_DIR/nexora" < /dev/tty || { rm -f "$TMP"; fail "Could not write to $BIN_DIR."; }
fi
ok "Installed $BIN_DIR/nexora"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf '\033[33m ! \033[0m%s\n' "$BIN_DIR is not on your PATH — add it to your shell profile:"
     echo "     export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

bold ""
bold "  NexoraCLI installed!"
bold ""
echo "  Connect to your instance:"
echo "    nexora login --url https://your-instance.example.com    # email/password"
echo "    nexora pair  --url https://your-instance.example.com    # code from web Settings → Devices"
echo ""
echo "  Then run:  nexora"
echo ""
