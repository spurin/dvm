#!/usr/bin/env bash
#
# build-windows.sh — cross-compile a fully-static Windows QEMU (x86_64) from
# source in a container (Fedora MinGW-w64), export to stage-windows-amd64/, and
# verify. No Windows OS required. Single source of truth for local + CI builds.
#
#   ./build-windows.sh        (windows/amd64)
#
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
# shellcheck source=versions.env
source ./versions.env

OUT="stage-windows-amd64"
echo ">> cross-compiling fully-static qemu-system-x86_64.exe (windows/amd64)"
rm -rf "$OUT"
docker buildx build -f Dockerfile.windows \
  --build-arg QEMU_VERSION="$QEMU_VERSION" \
  --build-arg QEMU_SHA256="$QEMU_SHA256" \
  --build-arg LIBSLIRP_VERSION="$LIBSLIRP_VERSION" \
  --build-arg LIBSLIRP_URL="$LIBSLIRP_URL" \
  --build-arg LIBSLIRP_SHA256="$LIBSLIRP_SHA256" \
  --build-arg ZSTD_VERSION="$ZSTD_VERSION" \
  --build-arg ZSTD_URL="$ZSTD_URL" \
  --build-arg ZSTD_SHA256="$ZSTD_SHA256" \
  --target export --output "type=local,dest=$OUT" \
  .

echo ">> staged: $OUT/stage/qemu-system-x86_64.exe"
echo ">> verifying"
./verify-windows.sh "$OUT/stage"
echo
echo "Done. Package with:  ./package.sh windows-amd64 $OUT/stage"
