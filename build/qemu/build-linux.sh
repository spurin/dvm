#!/usr/bin/env bash
#
# build-linux.sh — build a fully-static Linux QEMU in a container (Alpine/musl)
# and export the staged tree to stage-linux-<arch>/. Single source of truth for
# both local container builds and the CI workflow.
#
#   ./build-linux.sh [amd64|arm64]      (default: arm64)
#
# Building a non-native arch works via buildx/QEMU emulation but is slow; CI runs
# each arch on a native runner. Versions/checksums come from versions.env.
#
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
# shellcheck source=versions.env
source ./versions.env

ARCH="${1:-arm64}"
case "$ARCH" in
  amd64) QEMU_TARGET=x86_64-softmmu;  QBIN=qemu-system-x86_64 ;;
  arm64) QEMU_TARGET=aarch64-softmmu; QBIN=qemu-system-aarch64 ;;
  *) echo "usage: build-linux.sh [amd64|arm64]" >&2; exit 1 ;;
esac

OUT="stage-linux-${ARCH}"
echo ">> building fully-static qemu ($QEMU_TARGET) for linux/$ARCH"
rm -rf "$OUT"
docker buildx build -f Dockerfile.linux \
  --platform "linux/${ARCH}" \
  --build-arg QEMU_TARGET="$QEMU_TARGET" \
  --build-arg QEMU_VERSION="$QEMU_VERSION" \
  --build-arg QEMU_SHA256="$QEMU_SHA256" \
  --build-arg LIBSLIRP_VERSION="$LIBSLIRP_VERSION" \
  --build-arg LIBSLIRP_URL="$LIBSLIRP_URL" \
  --build-arg LIBSLIRP_SHA256="$LIBSLIRP_SHA256" \
  --target export --output "type=local,dest=$OUT" \
  .

echo ">> staged: $OUT/stage/bin/$QBIN"
echo ">> verifying"
./verify-linux.sh "$OUT/stage" "$QBIN"
echo
echo "Done. Package with:  ./package.sh linux-${ARCH} $OUT/stage"
