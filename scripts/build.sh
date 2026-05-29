#!/usr/bin/env bash
# Build Lattice: bundle the dashboard, embed it, cross-compile every target
# from ONE machine (mini-ops). Pure-Go SQLite (modernc.org/sqlite) means
# CGO_ENABLED=0 cross-compiles cleanly to every OS/arch — no per-machine
# toolchain (docs/DECISIONS.md D6). Output: dist/lattice-<os>-<arch>[.exe].
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
export PATH="/opt/homebrew/bin:$PATH"

VERSION="${LATTICE_VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X main.Version=${VERSION}"
EMBED_DIR="internal/hub/web/dist"

echo "==> lattice build ${VERSION}"

# 1. Dashboard → static bundle → embed dir
if [ -f dashboard/package.json ]; then
  echo "==> building dashboard"
  ( cd dashboard && (npm ci 2>/dev/null || npm install) && npm run build )
  rm -rf "${EMBED_DIR:?}"/*
  cp -R dashboard/dist/. "$EMBED_DIR"/
  echo "==> embedded $(find "$EMBED_DIR" -type f | wc -l | tr -d ' ') dashboard files"
else
  echo "==> WARN: no dashboard/package.json; embedding placeholder"
fi

# 2. Cross-compile (CGO off ⇒ portable static binaries from this one host)
mkdir -p dist
build() {
  local goos="$1" goarch="$2" ext="${3:-}"
  local out="dist/lattice-${goos}-${goarch}${ext}"
  echo "==> ${goos}/${goarch} -> ${out}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out" .
}

build darwin  arm64
build darwin  amd64
build windows amd64 .exe
build linux   amd64
build linux   arm64

echo "==> done:"
ls -lh dist/
