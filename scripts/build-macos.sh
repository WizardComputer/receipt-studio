#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [ "$(uname -s)" != Darwin ]; then
  echo "Creating the macOS app and DMG requires a Mac." >&2
  exit 1
fi
command -v go >/dev/null
command -v codesign >/dev/null
command -v hdiutil >/dev/null
if [ "${1:-}" = "--check" ]; then
  echo "macOS packaging prerequisites available; no build performed."
  exit 0
fi
exec bash scripts/build-mygo.sh -platform "darwin/${GOARCH:-$(go env GOARCH)}" "$@"
