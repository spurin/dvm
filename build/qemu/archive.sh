#!/usr/bin/env bash
#
# archive.sh - package a staged static QEMU tree into a distributable binary
# archive + SHA-256 checksum, for GitHub Releases / workflow artifacts.
#
#   ./archive.sh <platform> <stage-dir>
#   ./archive.sh linux-amd64 stage-linux-amd64/stage
#
# Produces (in build/qemu/):
#   qemu-<platform>-<qemu-version>.tar.gz
#   qemu-<platform>-<qemu-version>.tar.gz.sha256
#
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
# shellcheck source=versions.env
source ./versions.env

PLATFORM="${1:?usage: archive.sh <platform> <stage-dir>}"
STAGE_IN="${2:?usage: archive.sh <platform> <stage-dir>}"
STAGE="$(cd "$STAGE_IN" 2>/dev/null && pwd || true)"
[ -n "$STAGE" ] && [ -d "$STAGE" ] && [ -n "$(ls -A "$STAGE" 2>/dev/null)" ] || {
  echo "no staged build at $STAGE_IN (run the build first)" >&2; exit 1; }

TITLE="qemu-${PLATFORM}-${QEMU_VERSION}.tar.gz"
echo ">> archiving $STAGE -> $TITLE"
tar czf "$HERE/$TITLE" -C "$STAGE" .

# Portable sha256 (sha256sum on Linux, shasum on macOS).
if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$HERE" && sha256sum "$TITLE" > "$TITLE.sha256" )
else
  ( cd "$HERE" && shasum -a 256 "$TITLE" > "$TITLE.sha256" )
fi

echo ">> wrote:"
echo "   $HERE/$TITLE"
echo "   $HERE/$TITLE.sha256"
cat "$HERE/$TITLE.sha256"
