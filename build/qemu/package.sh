#!/usr/bin/env bash
#
# package.sh — package the staged static QEMU as an OCI artifact, mirroring the
# spurin/ubuntu-cloudimg layout: a payload layer (the tar.gz) + a text/plain
# sha256 sidecar + a provenance JSON layer. Builds a *local* OCI layout (no
# network); the registry push is printed but deferred.
#
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
# shellcheck source=versions.env
source ./versions.env

# Usage: package.sh [platform] [stage-dir]
#   platform   e.g. darwin-arm64 (default), linux-amd64, linux-arm64
#   stage-dir  staged QEMU tree (default: ./stage)
PLATFORM="${1:-darwin-arm64}"
STAGE_IN="${2:-$HERE/stage}"
STAGE="$(cd "$STAGE_IN" 2>/dev/null && pwd || true)"
# Accept either a bin/ subdir (macOS/Linux) or exes at the stage root (Windows).
[ -n "$STAGE" ] && [ -d "$STAGE" ] && [ -n "$(ls -A "$STAGE" 2>/dev/null)" ] || { echo "no staged build at $STAGE_IN (run the build first)" >&2; exit 1; }
command -v oras >/dev/null || { echo "missing oras (brew install oras)" >&2; exit 1; }

TITLE="qemu-${PLATFORM}-${QEMU_VERSION}.tar.gz"
TAG="${QEMU_VERSION}-${PLATFORM}"
REGISTRY="${REGISTRY:-docker.io/spurin/qemu}"
LAYOUT="$HERE/oci-layout"

# Media types (match the diveinto/spurin artifact convention).
MT_PAYLOAD="application/vnd.diveinto.qemu.v1+tar+gzip"
MT_PROVENANCE="application/vnd.diveinto.qemu.provenance.v1+json"
MT_CONFIG="application/vnd.diveinto.qemu.config.v1+json"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo ">> tarring staged tree -> $TITLE"
tar czf "$work/$TITLE" -C "$STAGE" .

echo ">> sha256 sidecar"
( cd "$work" && shasum -a 256 "$TITLE" > "$TITLE.sha256" )

echo ">> provenance"
clangv="$(clang --version 2>/dev/null | head -1 || echo n/a)"
sdkv="$(xcrun --show-sdk-version 2>/dev/null || echo n/a)"
sha="$(awk '{print $1}' "$work/$TITLE.sha256")"
cat > "$work/$TITLE.provenance.json" <<EOF
{
  "component": "qemu",
  "platform": "${PLATFORM}",
  "qemu_version": "${QEMU_VERSION}",
  "payload_sha256": "${sha}",
  "deps": {
    "libffi": "${LIBFFI_VERSION}", "pcre2": "${PCRE2_VERSION}",
    "glib": "${GLIB_VERSION}", "libslirp": "${LIBSLIRP_VERSION}", "zstd": "${ZSTD_VERSION}"
  },
  "source_sha256": {
    "qemu": "${QEMU_SHA256}", "libffi": "${LIBFFI_SHA256}", "pcre2": "${PCRE2_SHA256}",
    "glib": "${GLIB_SHA256}", "libslirp": "${LIBSLIRP_SHA256}", "zstd": "${ZSTD_SHA256}"
  },
  "toolchain": { "clang": "${clangv}", "macos_sdk": "${sdkv}" }
}
EOF

echo ">> building local OCI layout: $LAYOUT:$TAG"
rm -rf "$LAYOUT"
( cd "$work"
  oras push --oci-layout "$LAYOUT:$TAG" \
    --artifact-type "$MT_CONFIG" \
    "$TITLE:$MT_PAYLOAD" \
    "$TITLE.sha256:text/plain" \
    "$TITLE.provenance.json:$MT_PROVENANCE" )

# Keep the tarball next to the layout for convenience / CI upload.
cp "$work/$TITLE" "$HERE/$TITLE"

echo
echo ">> manifest:"
oras manifest fetch --oci-layout "$LAYOUT:$TAG" | sed 's/^/   /'
echo
echo "Artifacts:"
echo "  tarball:    $HERE/$TITLE"
echo "  OCI layout: $LAYOUT:$TAG"
echo
echo "Deferred push (run when ready, with registry auth):"
echo "  oras cp --from-oci-layout \"$LAYOUT:$TAG\" \"$REGISTRY:$TAG\""
