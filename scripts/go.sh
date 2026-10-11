#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export GOPATH="${GOPATH:-$root/.local/gopath}"
export GOCACHE="${GOCACHE:-$root/.local/cache/go}"
if [ -x "$root/.local/toolchains/go/bin/go" ]; then
  exec "$root/.local/toolchains/go/bin/go" "$@"
fi
if command -v go >/dev/null 2>&1 && go version 2>/dev/null | grep -q 'go1.27.2 '; then
  exec go "$@"
fi
printf '%s\n' 'Go 1.27.2 required. Run make setup or install that toolchain.' >&2
exit 1
