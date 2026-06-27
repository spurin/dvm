#!/usr/bin/env bash
#
# build-dvm.sh - build and codesign the macOS dvm binary with the
# Virtualization.framework entitlement, required for the native `--engine vz`.
#
# cgo must be enabled (the vz engine bridges Objective-C). A pure-Go build
# (CGO_ENABLED=0) still works but compiles without the vz engine (qemu only).
#
#   ./build-dvm.sh [output-path]   (default: dist/dvm)
#
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
OUT="${1:-$ROOT/dist/dvm}"

mkdir -p "$(dirname "$OUT")"
echo ">> building $OUT (CGO_ENABLED=1, includes --engine vz)"
( cd "$ROOT" && CGO_ENABLED=1 go build -o "$OUT" ./cmd/dvm )

echo ">> codesigning with com.apple.security.virtualization (adhoc)"
codesign --force --sign - --entitlements "$HERE/dvm.entitlements" "$OUT"
codesign -d --entitlements - "$OUT" 2>&1 | grep -qi virtualization \
  && echo ">> ok: virtualization entitlement present"

echo "Done: $OUT"
echo "For distribution, re-sign with a Developer ID instead of adhoc (-s -)."
