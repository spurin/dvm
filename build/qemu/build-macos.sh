#!/usr/bin/env bash
#
# build-macos.sh — build a statically-linked qemu-system-aarch64 + qemu-img for
# macOS/arm64 from upstream source, with all third-party deps linked statically
# (only Apple system libs/frameworks remain dynamic).
#
# Single source of truth for both local PoC builds and the CI workflow. Output:
#   build/qemu/stage/{bin,share/qemu}   (a relocatable QEMU tree)
#
# Prereqs (build-time only, not shipped): meson ninja pkg-config cmake
#   brew install meson ninja pkg-config cmake oras
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

# shellcheck source=versions.env
source ./versions.env
# shellcheck source=lib/deps.sh
source ./lib/deps.sh

export DEPS="$HERE/.deps"        # static dep prefix (.a only)
export WORK="$HERE/.work"        # source + build scratch
export STAGE="$HERE/stage"       # final relocatable QEMU tree
SRC="$WORK/src"
NPROC="$(sysctl -n hw.ncpu 2>/dev/null || echo 4)"
export NPROC

# Homebrew provides the build tools (meson/ninja/pkg-config/cmake); keep it on
# PATH for those, but point pkg-config at our prefix + /usr/lib only, so QEMU's
# auto-detection never picks up Homebrew libraries.
export PATH="/opt/homebrew/bin:$PATH"
export PKG_CONFIG_LIBDIR="$DEPS/lib/pkgconfig:/usr/lib/pkgconfig"
export PKG_CONFIG_PATH="$DEPS/lib/pkgconfig"

require_tools() {
  local missing=0 t
  for t in meson ninja pkg-config cmake clang; do
    command -v "$t" >/dev/null || { echo "missing build tool: $t" >&2; missing=1; }
  done
  [ "$missing" -eq 0 ] || { echo "Install: brew install meson ninja pkg-config cmake" >&2; exit 1; }
}

# fetch <url> <out> <sha256>
fetch() {
  local url="$1" out="$2" want="$3" path="$SRC/$2"
  if [ ! -f "$path" ]; then
    echo ">> fetching $out"
    curl -fsSL --retry 5 --retry-delay 3 --retry-all-errors "$url" -o "$path"
  fi
  local got; got="$(shasum -a 256 "$path" | awk '{print $1}')"
  [ "$got" = "$want" ] || { echo "checksum mismatch for $out: got $got want $want" >&2; exit 1; }
}

extract() { # <tarball> <destdir>
  local tarball="$1" dest="$2"
  [ -d "$dest" ] && return 0
  mkdir -p "$dest"
  tar xf "$SRC/$tarball" -C "$dest" --strip-components=1
}

fetch_all() {
  mkdir -p "$SRC"
  fetch "$LIBFFI_URL"   "libffi-${LIBFFI_VERSION}.tar.gz"   "$LIBFFI_SHA256"
  fetch "$PCRE2_URL"    "pcre2-${PCRE2_VERSION}.tar.bz2"    "$PCRE2_SHA256"
  fetch "$GLIB_URL"     "glib-${GLIB_VERSION}.tar.xz"       "$GLIB_SHA256"
  fetch "$LIBSLIRP_URL" "libslirp-${LIBSLIRP_VERSION}.tar.gz" "$LIBSLIRP_SHA256"
  fetch "$ZSTD_URL"     "zstd-${ZSTD_VERSION}.tar.gz"       "$ZSTD_SHA256"
  fetch "$QEMU_URL"     "qemu-${QEMU_VERSION}.tar.xz"       "$QEMU_SHA256"
}

extract_all() {
  mkdir -p "$WORK/build"
  extract "libffi-${LIBFFI_VERSION}.tar.gz"   "$WORK/build/libffi-${LIBFFI_VERSION}"
  extract "pcre2-${PCRE2_VERSION}.tar.bz2"    "$WORK/build/pcre2-${PCRE2_VERSION}"
  extract "glib-${GLIB_VERSION}.tar.xz"       "$WORK/build/glib-${GLIB_VERSION}"
  extract "libslirp-${LIBSLIRP_VERSION}.tar.gz" "$WORK/build/libslirp-${LIBSLIRP_VERSION}"
  extract "zstd-${ZSTD_VERSION}.tar.gz"       "$WORK/build/zstd-${ZSTD_VERSION}"
  extract "qemu-${QEMU_VERSION}.tar.xz"       "$WORK/build/qemu-${QEMU_VERSION}"
}

# QEMU configure flags. Enables only what dvm needs; everything pulling a
# Homebrew dylib is disabled (and would auto-disable anyway, since pkg-config
# can't see Homebrew). fdt uses QEMU's bundled dtc subproject.
qemu_configure_flags() {
  cat <<EOF
--target-list=aarch64-softmmu
--prefix=$STAGE
--enable-hvf --enable-slirp --enable-fdt=internal --enable-tools
--disable-pixman --disable-docs --disable-werror
--disable-gtk --disable-sdl --disable-vnc --disable-cocoa --disable-curses --disable-opengl
--disable-gnutls --disable-nettle --disable-gcrypt
--disable-capstone --disable-curl --disable-libssh
--disable-bzip2 --disable-snappy --disable-lzo
--disable-libusb --disable-usb-redir --disable-smartcard
--disable-vde --disable-vmnet --disable-coreaudio --disable-png --disable-auth-pam --disable-spice
--extra-cflags=-I$DEPS/include --extra-ldflags=-L$DEPS/lib
EOF
}

build_qemu() {
  local src="$WORK/build/qemu-${QEMU_VERSION}"
  echo ">> qemu ${QEMU_VERSION} (aarch64-softmmu, static deps)"
  ( cd "$src"
    rm -rf build-dvm; mkdir build-dvm; cd build-dvm
    # shellcheck disable=SC2046
    ../configure $(qemu_configure_flags)
    ninja
    rm -rf "$STAGE"
    ninja install )
}

write_buildinfo() {
  local out="$STAGE/BUILDINFO.json"
  local clangv sdkv
  clangv="$(clang --version | head -1)"
  sdkv="$(xcrun --show-sdk-version 2>/dev/null || echo unknown)"
  local flags; flags="$(qemu_configure_flags | tr '\n' ' ' | sed 's/  */ /g; s/^ //; s/ $//')"
  cat > "$out" <<EOF
{
  "component": "qemu",
  "host": "darwin-arm64",
  "qemu_version": "${QEMU_VERSION}",
  "deps": {
    "libffi": "${LIBFFI_VERSION}", "pcre2": "${PCRE2_VERSION}",
    "glib": "${GLIB_VERSION}", "libslirp": "${LIBSLIRP_VERSION}", "zstd": "${ZSTD_VERSION}"
  },
  "toolchain": { "clang": "${clangv}", "macos_sdk": "${sdkv}" },
  "configure_flags": "${flags}"
}
EOF
  echo ">> wrote $out"
}

main() {
  require_tools
  fetch_all
  extract_all
  mkdir -p "$DEPS"
  build_all_deps
  build_qemu
  write_buildinfo
  echo
  echo "Done. Static QEMU staged at: $STAGE/bin"
  echo "Next: ./verify.sh   (acceptance checks)"
}

main "$@"
