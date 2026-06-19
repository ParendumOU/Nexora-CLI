#!/usr/bin/env bash
# NexoraCLI build script (Linux/macOS/Git-Bash).
# Compiles inside the golang:1.23 container — no host Go toolchain required.
#
# Usage:
#   ./build.sh                  # build host binary -> bin/nexora
#   ./build.sh --all            # cross-compile all targets -> dist/
#   VERSION=0.3.0 ./build.sh    # stamp a version
#
# Requires Docker.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GO_IMAGE="${GO_IMAGE:-golang:1.23}"
VERSION="${VERSION:-0.2.1}"
LDFLAGS="-s -w -X main.version=${VERSION}"

# MSYS_NO_PATHCONV stops Git-Bash path mangling on Windows; harmless elsewhere.
run_go() { MSYS_NO_PATHCONV=1 docker run --rm -v "${REPO}:/app" -w //app "${GO_IMAGE}" sh -c "$1"; }

echo "==> go mod tidy"
run_go "go mod tidy"

if [[ "${1:-}" == "--all" ]]; then
  echo "==> cross-compiling all targets (v${VERSION})"
  run_go "\
    GOOS=linux   GOARCH=amd64 go build -ldflags '${LDFLAGS}' -o dist/nexora-linux-amd64 . && \
    GOOS=darwin  GOARCH=arm64 go build -ldflags '${LDFLAGS}' -o dist/nexora-darwin-arm64 . && \
    GOOS=windows GOARCH=amd64 go build -ldflags '${LDFLAGS}' -o dist/nexora-windows-amd64.exe ."
  echo "==> built into dist/"
  ls -lh "${REPO}/dist"
else
  echo "==> building host binary (v${VERSION})"
  run_go "go build -ldflags '${LDFLAGS}' -o bin/nexora ."
  echo "==> built bin/nexora"
fi
