#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "Building updater..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o updater .
echo "Build complete: updater"
