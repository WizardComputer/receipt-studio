#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
# MyGo otherwise silently skips Setup on non-Windows hosts without NSIS.
skip_installer=false
for arg in "$@"; do
  case "$arg" in
    -skip-installer|-skip-installer=true|-h|--help) skip_installer=true ;;
  esac
done
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) ;; # MyGo can download NSIS on Windows.
  *)
    if [ "$skip_installer" = false ] && ! command -v makensis >/dev/null 2>&1; then
      echo "Windows Setup requires NSIS (makensis). On macOS: brew install nsis." >&2
      echo "Use -skip-installer only when you explicitly need the executable without Setup." >&2
      exit 1
    fi
    ;;
esac
if [ "${1:-}" = "--check" ]; then
  command -v go >/dev/null
  echo "Windows packaging prerequisites available; no build performed."
  exit 0
fi
exec bash scripts/build-mygo.sh -platform windows/amd64 "$@"
