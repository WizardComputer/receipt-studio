#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Development process only: no release app, installer, signing or publishing.
exec go run -tags desktop ./cmd/receipt-studio --setup-autostart=false "$@"
