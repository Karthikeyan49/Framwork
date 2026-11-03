#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
if ! ./scripts/go.sh version >/dev/null 2>&1; then
  [ "$(uname -s)-$(uname -m)" = "Linux-x86_64" ] || { printf '%s\n' 'Install Go 1.27.3 for your platform and retry.' >&2; exit 1; }
  mkdir -p .local/toolchains
  archive=$(mktemp .local/toolchains/go.XXXXXX.tar.gz)
  trap 'rm -f "$archive"' EXIT INT TERM
  curl --fail --show-error --location --proto '=https' --tlsv1.2 https://go.dev/dl/go1.27.3.linux-amd64.tar.gz -o "$archive"
  printf '%s  %s\n' 65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40 "$archive" | sha256sum -c -
  tar -xzf "$archive" -C .local/toolchains
  rm -f "$archive"
  trap - EXIT INT TERM
fi
./scripts/go.sh mod download
./scripts/go.sh mod verify
test "$(node -p 'process.versions.node.split(".")[0]')" = 24
npm --cache "$root/.local/cache/npm" --prefix apps/web ci --ignore-scripts
