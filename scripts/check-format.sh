#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
gofmt_bin="$(./scripts/go.sh env GOROOT)/bin/gofmt"
files=$(find cmd internal db tests -name '*.go' -type f -exec "$gofmt_bin" -l {} +)
if [ -n "$files" ]; then printf '%s\n' "$files" >&2; exit 1; fi
