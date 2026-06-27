# shellcheck shell=bash
# Static dependency builders for the macOS QEMU build.
#
# Every dependency is installed into $DEPS as a static archive only (no .dylib),
# so that QEMU links them statically. Sourced by build-macos.sh, which exports:
#   DEPS  - install prefix (only .a libraries live here)
#   WORK  - build scratch dir (extracted sources under $WORK/build/<name>)
#   NPROC - parallelism
# and sets PKG_CONFIG_LIBDIR so only $DEPS + /usr/lib are searched (no Homebrew).

build_libffi() {
  local src="$WORK/build/libffi-${LIBFFI_VERSION}"
  echo ">> libffi ${LIBFFI_VERSION} (static)"
  ( cd "$src"
    ./configure --prefix="$DEPS" --enable-static --disable-shared --disable-docs
    make -j"$NPROC"
    make install )
}

build_pcre2() {
  local src="$WORK/build/pcre2-${PCRE2_VERSION}"
  echo ">> pcre2 ${PCRE2_VERSION} (static, pcre2-8)"
  ( cd "$src"
    ./configure --prefix="$DEPS" --enable-static --disable-shared --enable-pcre2-8 \
      --disable-pcre2grep-libz --disable-pcre2grep-libbz2
    make -j"$NPROC"
    make install )
}

build_glib() {
  local src="$WORK/build/glib-${GLIB_VERSION}"
  echo ">> glib ${GLIB_VERSION} (static, nls disabled)"
  ( cd "$src"
    rm -rf _build
    meson setup _build \
      --prefix="$DEPS" --buildtype=release --default-library=static \
      -Dnls=disabled -Dtests=false -Dglib_debug=disabled \
      -Dintrospection=disabled -Dman-pages=disabled \
      -Dlibmount=disabled -Dselinux=disabled \
      -Dc_args="-I$DEPS/include" -Dc_link_args="-L$DEPS/lib"
    ninja -C _build install )
}

build_libslirp() {
  local src="$WORK/build/libslirp-${LIBSLIRP_VERSION}"
  echo ">> libslirp ${LIBSLIRP_VERSION} (static; needs glib)"
  ( cd "$src"
    rm -rf _build
    meson setup _build \
      --prefix="$DEPS" --buildtype=release --default-library=static \
      -Dc_args="-I$DEPS/include" -Dc_link_args="-L$DEPS/lib"
    ninja -C _build install )
}

build_zstd() {
  local src="$WORK/build/zstd-${ZSTD_VERSION}"
  echo ">> zstd ${ZSTD_VERSION} (static)"
  ( cd "$src"
    cmake -S build/cmake -B _build \
      -DCMAKE_INSTALL_PREFIX="$DEPS" -DCMAKE_BUILD_TYPE=Release \
      -DZSTD_BUILD_SHARED=OFF -DZSTD_BUILD_STATIC=ON \
      -DZSTD_BUILD_PROGRAMS=OFF -DZSTD_BUILD_TESTS=OFF
    cmake --build _build -j"$NPROC"
    cmake --install _build )
}

build_all_deps() {
  build_libffi
  build_pcre2
  build_glib
  build_libslirp
  build_zstd
  # Safety net: there must be no dynamic libraries in the dep prefix.
  if ls "$DEPS"/lib/*.dylib >/dev/null 2>&1; then
    echo "ERROR: unexpected .dylib in dep prefix $DEPS/lib" >&2
    ls "$DEPS"/lib/*.dylib >&2
    return 1
  fi
}
