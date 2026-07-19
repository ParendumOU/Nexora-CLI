#!/usr/bin/env bash
# NexoraCLI installer — Linux / macOS
#
#   curl -fsSL https://raw.githubusercontent.com/ParendumOU/Nexora-CLI/main/install.sh | bash
#
# Installs the `nexora` binary and puts it on your PATH. No runtime deps. Re-run to update.
#
# Zero-touch join (what an admin's invite one-liner does):
#   curl -fsSL .../install.sh | bash -s -- --join <INVITE_TOKEN> --url https://your-instance
# Same via env: NEXORA_JOIN_TOKEN, NEXORA_URL.
#
# Overrides: NEXORA_CLI_VERSION (e.g. v0.19.0), NEXORA_CLI_BIN_DIR
set -euo pipefail

REPO="ParendumOU/Nexora-CLI"
API="https://api.github.com/repos/$REPO"

info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '\033[33m!\033[0m %s\n' "$*"; }
fail() { printf '\033[31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

# ── Args / env ──────────────────────────────────────────────────────────────────
JOIN_TOKEN="${NEXORA_JOIN_TOKEN:-}"
JOIN_URL="${NEXORA_URL:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --join)   JOIN_TOKEN="${2:-}"; shift 2 ;;
    --join=*) JOIN_TOKEN="${1#*=}"; shift ;;
    --url)    JOIN_URL="${2:-}"; shift 2 ;;
    --url=*)  JOIN_URL="${1#*=}"; shift ;;
    *) shift ;;
  esac
done

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

# ── Latest version ────────────────────────────────────────────────────────────
TAG="${NEXORA_CLI_VERSION:-}"
if [ -z "$TAG" ]; then
  TAG="$(curl -fsSL "$API/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  [ -n "$TAG" ] || fail "Could not determine the latest release. Set NEXORA_CLI_VERSION=vX.Y.Z and re-run."
fi

# ── Download ──────────────────────────────────────────────────────────────────
BASE="https://github.com/$REPO/releases/download/$TAG"
TMP="$(mktemp)"
FOUND=""
for ASSET in "nexora-$TAG-$OS-$ARCH" "nexora-$OS-$ARCH"; do
  if curl -fsSL -o "$TMP" "$BASE/$ASSET" 2>/dev/null; then FOUND="$ASSET"; break; fi
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
  if [ -w /usr/local/bin ] 2>/dev/null; then
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
  sudo mv "$TMP" "$BIN_DIR/nexora" < /dev/tty || { rm -f "$TMP"; fail "Could not write to $BIN_DIR."; }
fi
ok "Installed nexora $TAG → $BIN_DIR/nexora"

# ── PATH: add to the user's shell rc if missing ─────────────────────────────────
ensure_on_path() {
  case ":$PATH:" in *":$BIN_DIR:"*) return 0 ;; esac
  local rc
  case "$(basename "${SHELL:-sh}")" in
    zsh)  rc="$HOME/.zshrc" ;;
    bash) rc="$HOME/.bashrc" ;;
    *)    rc="$HOME/.profile" ;;
  esac
  if ! { [ -f "$rc" ] && grep -qF "$BIN_DIR" "$rc" 2>/dev/null; }; then
    printf '\n# Added by NexoraCLI installer\nexport PATH="%s:$PATH"\n' "$BIN_DIR" >> "$rc"
  fi
  export PATH="$BIN_DIR:$PATH"
  RELOAD_RC="$rc"
}
RELOAD_RC=""
ensure_on_path

# ── Join (auto) or print manual next-steps ──────────────────────────────────────
if [ -n "$JOIN_TOKEN" ] && [ -n "$JOIN_URL" ]; then
  info "Connecting to $JOIN_URL"
  "$BIN_DIR/nexora" join --url "$JOIN_URL" --token "$JOIN_TOKEN" \
    || fail "Could not join. The invite may be expired or already used — ask your admin for a new one."
  echo ""
  ok "You're all set."
  if [ -n "$RELOAD_RC" ]; then
    echo "  Run this once (or open a new terminal):  source $RELOAD_RC"
  fi
  echo "  Then start Nexora with:  nexora"
  echo ""
else
  echo ""
  echo "Connect to your instance:"
  echo "  nexora join  --url https://your-instance --token <INVITE_TOKEN>   # from an admin invite"
  echo "  nexora login --url https://your-instance                          # email/password"
  [ -n "$RELOAD_RC" ] && echo "" && warn "$BIN_DIR was added to PATH in $RELOAD_RC — run: source $RELOAD_RC"
  echo ""
fi
