#!/usr/bin/env bash
# Build Lattice: bundle the dashboard, embed it, cross-compile every target
# from ONE machine (the hub host). Pure-Go SQLite (modernc.org/sqlite) means
# CGO_ENABLED=0 cross-compiles cleanly to every OS/arch - no per-machine
# toolchain (docs/DECISIONS.md D6). Output: dist/lattice-<os>-<arch>[.exe].
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
export PATH="/opt/homebrew/bin:$PATH"

VERSION="${LATTICE_VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X main.Version=${VERSION}"
EMBED_DIR="internal/hub/web/dist"

echo "==> lattice build ${VERSION}"

# 1. Dashboard -> static bundle -> embed dir
if [ -f dashboard/package.json ]; then
  echo "==> building dashboard"
  # npm ci ONLY (no `|| npm install` fallback): a transient ci failure must abort
  # the build, never silently resolve off-lockfile deps into the embedded SPA.
  ( cd dashboard && npm ci && npm run build )
  # Defense-in-depth before the destructive wipe: EMBED_DIR must live under $ROOT.
  case "$ROOT/$EMBED_DIR" in
    "$ROOT"/*) : ;;
    *) echo "==> refusing to wipe EMBED_DIR outside ROOT: $EMBED_DIR" >&2; exit 1 ;;
  esac
  rm -rf "${EMBED_DIR:?}"/*
  cp -R dashboard/dist/. "$EMBED_DIR"/
  echo "==> embedded $(find "$EMBED_DIR" -type f | wc -l | tr -d ' ') dashboard files"
else
  echo "==> WARN: no dashboard/package.json; embedding placeholder"
fi

# 2. Cross-compile (CGO off => portable static binaries from this one host)
# Verify module checksums against go.sum before building - abort if the module
# cache has been tampered with or a dep doesn't match the lockfile.
echo "==> go mod verify"
go mod verify

mkdir -p dist
build() {
  local goos="$1" goarch="$2" ext="${3:-}"
  local out="dist/lattice-${goos}-${goarch}${ext}"
  echo "==> ${goos}/${goarch} -> ${out}"
  # -mod=readonly: never auto-mutate go.mod/go.sum during a release build.
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -mod=readonly -trimpath -ldflags "$LDFLAGS" -o "$out" .
}

build darwin  arm64
build darwin  amd64
build windows amd64 .exe
build linux   amd64
build linux   arm64

echo "==> done:"
ls -lh dist/
