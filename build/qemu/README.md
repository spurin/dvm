# Static QEMU builds for `dvm`

Builds **statically-linked QEMU + `qemu-img` from upstream source**, packaged as
an OCI artifact, so `dvm` can ship a relocatable QEMU instead of relying on a
host install. Two platforms:

| Platform | How | Result |
|----------|-----|--------|
| **macOS / arm64** | `build-macos.sh` (local/CI on a mac) | static deps; only Apple system libs dynamic |
| **Linux / amd64 + arm64** | `build-linux.sh` (Docker, Alpine/musl) | **truly fully static** (musl libc included; zero dynamic deps) |
| **Windows / amd64** | `build-windows.sh` (Docker, Fedora MinGW cross) | static deps; only Windows system DLLs imported (single self-contained .exe) |

## macOS (static deps, Apple system libs dynamic)

## What "static" means on macOS

A *fully* static binary is impossible on macOS (no static `libSystem`). Here it
means **all third-party libraries are statically linked** (glib, libslirp,
pcre2, libffi, zstd), leaving **only Apple-provided system libs/frameworks**
dynamic (`libSystem`, `libz`, `libiconv`, `libobjc`, `Hypervisor`,
`CoreFoundation`, `Foundation`, `IOKit`, …). The result has **zero `/opt/homebrew`
references** and runs on any Mac without Homebrew.

Acceptance check (run by `verify.sh`):

```
otool -L stage/bin/qemu-system-aarch64   # only /usr/lib/* and /System/Library/Frameworks/*
```

## Prerequisites (build-time only, not shipped)

```sh
brew install meson ninja pkg-config cmake oras
```

Xcode Command Line Tools (clang + macOS SDK) are also required. The dependency
prefix (`.deps`), source/scratch (`.work`), staged tree (`stage`), tarball and
`oci-layout` are all gitignored.

## Usage

```sh
./build-macos.sh     # fetch+verify pinned sources, build static deps + QEMU -> stage/
./verify.sh          # acceptance: otool static check + HVF/libslirp compiled-in (no HVF needed)
./package.sh         # tar stage/ + build a local OCI artifact (push deferred)
```

Versions and source checksums are pinned in [`versions.env`](versions.env); the
per-dependency build steps live in [`lib/deps.sh`](lib/deps.sh).

### HVF boot-parity test (real Mac only)

Hosted CI runners lack HVF/nested virt, so booting a guest must be done on a real
Mac. With `dvm` built (`go build -o dist/dvm ./cmd/dvm` at the repo root):

```sh
dvm start --config examples/dvm.yaml \
  --qemu-dir build/qemu/stage/bin --guest-password lab
ssh -p 2222 ubuntu@127.0.0.1      # password: lab
```

## What gets built

- `aarch64-softmmu` only (matches `dvm`'s native-guest strategy) + `qemu-img`.
- QEMU is configured with `--enable-hvf --enable-slirp --enable-fdt=internal`
  (bundled dtc) and `--disable-pixman` plus a long disable list; anything that
  would pull a Homebrew dylib is off (and auto-disables anyway, since
  `PKG_CONFIG_LIBDIR` points only at our static prefix + `/usr/lib`).

## Linux (fully static, via Docker)

Unlike macOS, **musl supports real fully-static linking**, so the Linux binary
has *no* dynamic dependencies at all (musl libc included) and runs on any distro.
The build runs in a container - Docker is the only prerequisite.

```sh
./build-linux.sh arm64     # or amd64  -> stage-linux-<arch>/  + runs verify-linux.sh
./package.sh linux-arm64 stage-linux-arm64/stage
```

[`Dockerfile.linux`](Dockerfile.linux) (Alpine/musl) uses Alpine's static
packages for glib/pcre2/libffi/zlib/zstd/libmount, builds **libslirp** from
source (the one dep Alpine doesn't ship static), and configures QEMU `--static
--enable-kvm --enable-slirp --enable-fdt=internal --enable-strip`. The
`QEMU_TARGET` is chosen from the arch (`x86_64-softmmu` / `aarch64-softmmu`).

`verify-linux.sh` runs the checks in an Alpine container matching the binary's
arch, asserting it's fully static (`file` → `static-pie linked`, no INTERP) and
that KVM + libslirp are compiled in. Building a non-native arch works via buildx
emulation but is slow; CI builds each arch on a native runner. The KVM *boot*
test needs a real Linux host with `/dev/kvm` (Docker Desktop's VM doesn't expose
it).

## Windows (static .exe, cross-compiled via Docker)

Cross-compiled from source with the **Fedora MinGW-w64** toolchain - no Windows
OS required. Fedora ships static mingw libs for glib/pcre2/libffi/zlib/gettext/
winpthreads/win-iconv; zstd and libslirp are cross-built static from source via
Fedora's `mingw64-cmake`/`mingw64-meson` wrappers. The result is a **single
self-contained `qemu-system-x86_64.exe`** that imports only Windows system DLLs
(KERNEL32, WS2_32, ucrtbase, …) - no bundled DLLs, no installer.

```sh
./build-windows.sh     # -> stage-windows-amd64/  + runs verify-windows.sh
./package.sh windows-amd64 stage-windows-amd64/stage
```

[`Dockerfile.windows`](Dockerfile.windows) configures QEMU `--cross-prefix=
x86_64-w64-mingw32- --static --enable-whpx --enable-slirp --enable-fdt=internal`
and defines `LIBSLIRP_STATIC` + `GLIB_STATIC_COMPILATION` so the static libs link
without dllimport stubs. The large edk2 UEFI firmware (~305 MB, unused for
direct-kernel boot) is trimmed. The Windows layout puts the exes at the folder
root with `share/` alongside, so `dvm --qemu-dir <extracted-folder>` runs it.

`verify-windows.sh` runs the acceptance gate anywhere (incl. this Mac) via the
mingw `objdump` in a container: every imported DLL must be a Windows system DLL.
Run-time `--version`/`-accel help` (whpx) checks use Wine on native amd64 / CI;
the real WHPX boot needs a Windows host with the Windows Hypervisor Platform
feature enabled.

This build (QEMU 11.0.1) has been **verified end-to-end on a real Windows host**:
accelerated boot under `accel=whpx`, cloud-init networking, host→guest port
forwarding and graceful shutdown, against `spurin/ubuntu-cloudimg-24.04`
(amd64). Note that under WHPX `dvm` selects `-cpu qemu64`: `-cpu host` and
`-cpu max` are rejected by WHPX (max advertises newer CPUID features such as APX
that it cannot virtualize, yielding an immediate "Unexpected VP exit code 4").

## Artifact layout

`package.sh` produces an OCI artifact mirroring the `spurin/ubuntu-cloudimg`
convention:

| Layer | Media type | Notes |
|-------|------------|-------|
| payload | `application/vnd.diveinto.qemu.v1+tar+gzip` | `stage/` tarred (bin/ + share/qemu) |
| checksum | `text/plain` | `<title>.sha256` sidecar |
| provenance | `application/vnd.diveinto.qemu.provenance.v1+json` | qemu+dep versions, source sha256s, toolchain |

The staged tree is self-locating: QEMU finds its data dir at `<bin>/../share/qemu`,
so `dvm --qemu-dir <extracted>/bin` works unchanged.

Deferred registry push (after validation, with auth):

```sh
oras cp --from-oci-layout build/qemu/oci-layout:<tag> docker.io/spurin/qemu:<tag>
```

## CI

- [`.github/workflows/qemu-macos.yml`](../../.github/workflows/qemu-macos.yml) -   `build-macos.sh` → `verify.sh` → `package.sh` on a `macos-14` runner.
- [`.github/workflows/qemu-linux.yml`](../../.github/workflows/qemu-linux.yml) -   `build-linux.sh` (Docker) for amd64 + arm64 on native Linux runners, then
  `package.sh`.
- [`.github/workflows/qemu-windows.yml`](../../.github/workflows/qemu-windows.yml) -   `build-windows.sh` (Docker, MinGW cross) on `ubuntu-latest`, with a Wine
  run-check, then `package.sh`.

All upload the artifact and prove static linkage + that the accelerator
(HVF/KVM/WHPX) and libslirp are compiled in. The accelerated *boot* test stays
manual (hosted runners lack HVF/KVM/WHP nested virt).

## Future targets

- `darwin/amd64` - same `build-macos.sh` on a `macos-13` (x86) runner.
- `windows/arm64` - MinGW aarch64 cross (toolchain less mature).
- Registry push of all artifacts once validated.
