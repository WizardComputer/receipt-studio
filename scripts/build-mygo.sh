#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Run only when a release build is explicitly requested. A native build must
# include desktop; otherwise the agent deliberately runs headless.
export GOFLAGS="${GOFLAGS:-} -tags=desktop"
exec go run github.com/egoist/mygo/cmd/mygo@v0.9.1 build "$@"
