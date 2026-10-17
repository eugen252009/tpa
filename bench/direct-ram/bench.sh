#!/bin/sh
# Thin entrypoint for the existing comparative harness, not a second framework.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"
exec python3 bench/compare-direct.py "$@"
