# dvm - a cross-platform CLI VM runner driven by OCI artifacts

`dvm` is a lightweight, single-binary launcher that boots a virtual machine from
components (kernel, initrd, rootfs) pulled as **OCI artifacts**, and forwards
guest ports to the host on `localhost`. Think of it as a Firecracker-style VM
runner, but for any host OS, shipped as one convenient binary.

- **Standalone and self-contained** - download the single `dvm` binary, run it
  from any directory, and it does the rest: it picks the best engine for the
  host, downloads a static QEMU itself when one is needed, and keeps everything
  in a local `.cache` / `.state` beside where you run it.
- No embedded QEMU/kernel/rootfs - the binary stays small and pulls what it
  needs, digest-addressed and cached.
- Works against any OCI registry. Components can be **multi-arch (cross-arch)**
  tags (preferred - one tag, dvm picks the entry matching the guest arch), direct
  single-arch tags, or plain local files.
- **Configurable from a file or a URL** - put any flag in a YAML config and load
  it with `--config`, from a local path or an **http(s) URL** so a whole team can
  share one config (`dvm start --config https://example.com/dvm.yaml`). See
  [Configuration](#configuration-config-files).

> Status: verified end-to-end on real hardware against
> `docker.io/spurin/ubuntu-cloudimg-24.04` (accelerated boot, cloud-init
> networking, host to guest port forwarding, graceful shutdown):
> macOS/arm64 (HVF, plus the native `vz` engine), Linux/amd64 (KVM, with a TCG
> software-emulation fallback) and Windows/amd64 (QEMU + WHPX). The same code
> covers the remaining arch/OS combinations.

## How it works

```
dvm  ->  pull OCI components (oras)  ->  cache (content-addressed)
     ->  pick engine: native vz on capable macOS, else qemu (auto-downloaded)
     ->  qcow2/raw overlay + cloud-init seed
     ->  qemu-system-<arch> (HVF/KVM/WHPX/TCG)  [or vz directly]
     ->  user-mode net + hostfwd  ->  localhost:PORT
```

Components are referenced individually and may be OCI refs or local paths. The
examples below use the **cross-arch** tags - the same reference works on Intel
and ARM hosts because dvm resolves the multi-arch index to the guest's arch:

| Component | Flag         | Example |
|-----------|--------------|---------|
| kernel    | `--kernel`   | `oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-vmlinux` |
| initrd    | `--initrd`   | `oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-initrd` |
| rootfs    | `--rootfs`   | `oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-qcow2` |
| qemu      | `--qemu-dir` | optional - dvm auto-downloads a static QEMU when needed (see Install) |

Need a specific architecture regardless of the host? Use the arch-suffixed tags
(`...-vmlinux-amd64`, `...-vmlinux-arm64`) and set `--guest-arch` to match.

## Install

Download the single `dvm` binary for your platform and run it - nothing else is
required. dvm fetches a static QEMU on its own the first time the qemu engine
needs one. Each [release](https://github.com/spurin/dvm/releases) publishes a
`dvm` binary for every platform, a statically-compiled QEMU for each (used by the
auto-download), and a `SHA256SUMS`:

- `dvm-darwin-arm64`, `dvm-darwin-amd64` (built with the native `vz` engine)
- `dvm-linux-amd64`, `dvm-linux-arm64`
- `dvm-windows-amd64.exe`

```sh
# example: Apple Silicon
curl -fsSL -o dvm https://github.com/spurin/dvm/releases/latest/download/dvm-darwin-arm64
chmod +x dvm
xattr -dr com.apple.quarantine ./dvm     # release binaries are adhoc-signed
./dvm start --kernel ... --initrd ... --rootfs ...   # QEMU is fetched if needed
```

About the QEMU it downloads: it is **compiled from source, statically linked with
no external dependencies**, and provided purely for convenience. dvm caches it
under `.cache/qemu/` and reuses it. You never have to install or manage QEMU
yourself - but you can: pass `--qemu-dir` at any directory containing
`qemu-system-*` and `qemu-img` (for example `brew install qemu` then
`--qemu-dir /opt/homebrew/opt/qemu/bin`, or a `qemu-<platform>.tar.gz` release
tarball you extracted), or build it from the harness in [`build/qemu/`](build/qemu/).
On macOS the `vz` engine needs no QEMU at all.

## What a run looks like

dvm narrates what it is doing as it goes. A first run on macOS (no `--qemu-dir`,
so it fetches a static QEMU once, then caches it):

```text
$ dvm start \
    --kernel oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-vmlinux \
    --initrd oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-initrd \
    --rootfs oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-qcow2 \
    --ssh-port 2222 --guest-password lab

☁️  Fetching static QEMU (qemu-darwin-arm64-11.0.1.tar.gz); this happens once and is cached...
📦 Resolving components...
☁️  Pulling kernel (docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-vmlinux)...
☁️  Pulling initrd (docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-initrd)...
☁️  Pulling rootfs (docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-qcow2)...
🚀 Starting dvm...
⚡ Using QEMU accelerator: hvf

🔌 Services:
  ssh: ssh -p 2222 ubuntu@127.0.0.1

🟢 Running. Press Ctrl-C to stop.
🛑 Shutting down...
👋 VM stopped.
```

On later runs the QEMU and the OCI components are already cached, so it jumps
straight to `🚀 Starting`. With `--engine vz` you would see
`🚀 Started dvm (Virtualization.framework)` and no QEMU fetch.

## Quick start (macOS / Apple Silicon)

```sh
REG=docker.io/spurin/ubuntu-cloudimg-24.04
./dvm start \
  --kernel oci://$REG:6.8.0-124-generic-vmlinux \
  --initrd oci://$REG:6.8.0-124-generic-initrd \
  --rootfs oci://$REG:6.8.0-124-generic-qcow2 \
  --ssh-port 2222 --guest-password lab \
  --console
```

No `--qemu-dir` and no `--engine`, so dvm decides for itself. On macOS the
default `--engine auto` chooses based on the rootfs you give it, because the
native `vz` engine cannot read qcow2 and needs a raw image:

| Rootfs you pass | Engine chosen | What happens |
|-----------------|---------------|--------------|
| `--rootfs` qcow2 (as above) | **qemu** (HVF) | vz can't use qcow2, so dvm auto-downloads a static QEMU and runs it under HVF |
| `--rootfs` raw/ext4, or `--rootfs-vz` raw/ext4 | **vz** (native) | a raw rootfs is vz-compatible, so dvm uses Virtualization.framework directly - no QEMU download |
| both `--rootfs` qcow2 and `--rootfs-vz` ext4 | **vz** (native) | macOS prefers vz and uses `--rootfs-vz`; the qcow2 `--rootfs` is what the same command would use under qemu on Linux/Windows |

So the example above runs under qemu. To use the native engine instead, add a
raw rootfs, for example `--rootfs-vz oci://$REG:6.8.0-124-generic-ext4` (the vz
engine needs a codesigned `dvm` - the release binaries are). You can always
override the decision with `--engine qemu` or `--engine vz`.

`--console` attaches your terminal to the guest serial console (press **Ctrl-]**
to detach; the VM keeps running). Or omit it to run headless and SSH in:

```sh
ssh -p 2222 ubuntu@127.0.0.1     # password: lab
```

## Quick start (Linux / KVM)

```sh
REG=docker.io/spurin/ubuntu-cloudimg-24.04
./dvm start \
  --kernel oci://$REG:6.8.0-124-generic-vmlinux \
  --initrd oci://$REG:6.8.0-124-generic-initrd \
  --rootfs oci://$REG:6.8.0-124-generic-qcow2 \
  --ssh-port 2222 --guest-password lab
```

`dvm` logs `Using QEMU accelerator: kvm` when `/dev/kvm` is usable. Without it,
it falls back to software emulation (TCG) - the guest still boots, just slower,
and dvm prints how to enable KVM (firmware virtualization plus, for non-root
users, joining the `kvm` group).

## Quick start (Windows / WHPX)

The Windows engine is QEMU accelerated by the **Windows Hypervisor Platform
(WHPX)**, falling back to software emulation (TCG) when WHPX is unavailable.

**1. Enable WHPX** - one-time, in an **admin** PowerShell, then **reboot**:

```powershell
dism /Online /Enable-Feature /FeatureName:HypervisorPlatform /All
```

WHPX also needs CPU virtualization (VT-x/AMD-V) enabled in firmware. `dvm` logs
`Using QEMU accelerator: whpx` once it is active; otherwise it falls back to TCG
and prints exactly how to enable WHPX. (Note: Task Manager / `Win32_Processor`
may report virtualization as disabled when Hyper-V is already running - that is a
reporting artifact, not the real state; the definitive check is the `whpx` log
line.)

**2. Run** (PowerShell) - dvm downloads a static QEMU itself, no setup needed:

```powershell
$REG = "docker.io/spurin/ubuntu-cloudimg-24.04"
.\dvm.exe start `
  --kernel "oci://${REG}:6.8.0-124-generic-vmlinux" `
  --initrd "oci://${REG}:6.8.0-124-generic-initrd" `
  --rootfs "oci://${REG}:6.8.0-124-generic-qcow2" `
  --ssh-port 2222 --guest-password lab
```

```powershell
ssh -p 2222 ubuntu@127.0.0.1     # password: lab
```

The Windows specifics are automatic - no extra flags: under WHPX `dvm` uses
`-cpu qemu64` (WHPX rejects `-cpu host`/`-cpu max`, which expose CPUID features
WHPX cannot virtualize), and the QMP monitor plus serial console run over
loopback TCP instead of unix sockets.

## Guest resources (CPU and memory)

The guest defaults to **2 vCPUs and 2048 MB of RAM**. Override them per run with
`--cpus` and `--memory` (in MB), which work the same on every engine and OS:

```sh
REG=docker.io/spurin/ubuntu-cloudimg-24.04
./dvm start \
  --kernel oci://$REG:6.8.0-124-generic-vmlinux \
  --initrd oci://$REG:6.8.0-124-generic-initrd \
  --rootfs oci://$REG:6.8.0-124-generic-qcow2 \
  --cpus 4 --memory 8192 \
  --ssh-port 2222 --guest-password lab
```

Or set them in a config file:

```yaml
guest:
  cpus: 4
  memory_mb: 8192
```

## Running and stopping

`dvm start` (and `dvm console`) run in the **foreground** and supervise the VM:

- **Stop it:** press **Ctrl-C** in that terminal. dvm asks the guest to power off
  gracefully (ACPI), escalating to terminate/kill if it does not exit in time. In
  `--console` mode, press **Ctrl-]** first to detach the console, then Ctrl-C.
- **Check on it:** from another terminal, `dvm status` reports whether the VM is
  running and which ports are exposed.

There is no detached/background mode yet, so there is no `dvm stop` - stopping is
Ctrl-C on the foreground process. State persists by default; use `--no-persist`
for an ephemeral run or `--reset` to recreate the writable overlay.

## Configuration (config files)

Every flag can come from a YAML config file, so you do not have to retype long
component references. Resolution order (later overrides earlier):

1. built-in defaults
2. the config file: `--config <path-or-url>` if given, otherwise `./dvm.yaml`,
   otherwise the file in your OS user config dir
3. command-line flags

`--config` accepts a local path or an **http(s) URL**, so a team can share one
config: `dvm start --config https://example.com/dvm.yaml`.

A starter config is in [`examples/dvm.yaml`](examples/dvm.yaml) and is runnable
as-is:

```sh
dvm start --config examples/dvm.yaml --guest-password lab
# stop with Ctrl-C; from another terminal: dvm status
```

```yaml
# excerpt - see examples/dvm.yaml for the fully commented version
name: dvm
components:
  kernel:    oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-vmlinux
  initrd:    oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-initrd
  rootfs:    oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-qcow2   # qemu
  rootfs_vz: oci://docker.io/spurin/ubuntu-cloudimg-24.04:6.8.0-124-generic-ext4    # vz (raw)
  # qemu_dir is optional - omit it and dvm auto-downloads a static QEMU
guest:
  arch: arm64           # x86_64 | arm64 (defaults to the host's native arch)
  memory_mb: 2048
  cpus: 2
network:
  ssh_port: 2222
```

Because the component tags are cross-arch and `rootfs`/`rootfs_vz` cover both
engines (see below), **the same config file works on macOS, Linux and Windows**.

## Engines

`dvm` chooses an engine automatically (`--engine auto`, the default) and you can
force one with `--engine`:

| Engine | Platforms | How |
|--------|-----------|-----|
| `auto` (default) | all | native `vz` on capable macOS when a raw rootfs is configured, otherwise `qemu` |
| `qemu` | macOS, Linux, Windows | static QEMU (auto-downloaded if no `--qemu-dir`); user-mode net + hostfwd; qcow2 or raw overlay |
| `vz` | macOS only | Apple **Virtualization.framework** (`Code-Hex/vz`); no QEMU, near-native speed |

`auto` prefers the native macOS hypervisor (no QEMU to download) when
Virtualization.framework is available (macOS 12+) and a vz-compatible raw rootfs
is configured (`rootfs_vz`, or a `rootfs` that looks like a raw/ext4 image);
otherwise it uses `qemu`.

### Native macOS engine (`--engine vz`)

Uses Apple's Virtualization.framework directly (the same hypervisor QEMU's HVF
accel sits on), so there is **no QEMU to ship or download**. Boots the same
kernel/initrd and cloud-init seed; networking is VZ NAT, with selected ports
exposed via per-port host TCP proxies to the guest IP (from the host ARP table).

Requirements:
- **macOS 12+**, and `dvm` must be **built with cgo and codesigned** with the
  `com.apple.security.virtualization` entitlement (the release `dvm-darwin-*`
  binaries already are):
  ```sh
  build/macos/build-dvm.sh        # CGO_ENABLED=1 build + adhoc codesign
  ```
  (A pure-Go `CGO_ENABLED=0` build still works but omits the vz engine.)
- The rootfs must be a **raw** disk image (the spurin `ext4` variant); see Rootfs
  formats below.

```sh
REG=docker.io/spurin/ubuntu-cloudimg-24.04
dvm start --engine vz \
  --kernel oci://$REG:6.8.0-124-generic-vmlinux \
  --initrd oci://$REG:6.8.0-124-generic-initrd \
  --rootfs oci://$REG:6.8.0-124-generic-ext4 \
  --kernel-cmdline "console=hvc0 root=/dev/vda1 rw" \
  --ssh-port 2222 --guest-password lab
```

## Rootfs formats (qcow2 vs raw/ext4)

| Engine | qcow2 | raw / ext4 |
|--------|:-----:|:----------:|
| `qemu` (macOS, Linux, Windows) | yes | yes |
| `vz` (macOS native) | no | yes (required) |

- The **qemu** engine accepts **either** format - it detects the base image type
  and builds the writable overlay accordingly. qcow2 is the smaller download and
  the usual choice on Linux/Windows.
- The **vz** engine cannot read qcow2 (Virtualization.framework limitation), so it
  needs a **raw** image (the spurin `ext4` variant); the overlay is an APFS
  copy-on-write clone.
- To keep one config portable, set both `rootfs` (qcow2 for qemu) and `rootfs_vz`
  (raw ext4 for vz). dvm uses `rootfs_vz` only for `--engine vz`, falling back to
  `rootfs` if it is unset. The equivalent flags are `--rootfs` and `--rootfs-vz`.

## Caching and storage

dvm is **self-contained per directory**: by default it keeps everything in a
`.cache` and `.state` folder beside where you run it, so you can download the
binary and use it from anywhere without touching shared or home locations.

- **`.cache/`** - pulled kernel/initrd/rootfs blobs (content-addressed, pulled
  once and reused, verified against their digest/checksum) and the auto-downloaded
  QEMU under `.cache/qemu/`. Cleared by `dvm clean`.
- **`.state/`** - the writable copy-on-write overlay, cloud-init seed, logs and
  pid. Cleared by `dvm clean --with-state`, or per run with `--reset` /
  `--no-persist`.

The base rootfs is **never modified** - writes go to the overlay, which persists
between runs by default. Point these elsewhere (for example a scratch disk) with
`--cache-dir` and `--data-dir`.

## Commands

| Command         | Description |
|-----------------|-------------|
| `dvm start`     | Boot the VM and supervise it in the foreground (default command) |
| `dvm console`   | Boot with the terminal attached to the guest serial console |
| `dvm status`    | Show whether the VM is running and its exposed ports |
| `dvm clean`     | Remove the cached assets (`--with-state` also clears VM state) |
| `dvm version`   | Print version information |

## Key flags

```
--engine auto|qemu|vz            engine; auto picks native macOS vz when usable, else qemu
--kernel/--initrd/--rootfs REF   component references (OCI ref or local path)
--rootfs-vz REF                  raw/ext4 rootfs for --engine vz (defaults to --rootfs)
--qemu-dir PATH                  use this QEMU instead of the auto-downloaded one
--guest-arch x86_64|arm64        guest architecture (defaults to host-native)
--memory MB / --cpus N           guest RAM (MB) and vCPU count (default 2048 / 2)
--port HOST:GUEST                add a forward (repeatable), e.g. 8080:80
--ssh-port PORT                  expose guest sshd on localhost:PORT
--ip-config MODE                 cloud-init | kernel-dhcp | kernel-static | none
--guest-password PW / --ssh-key  cloud-init login credentials
--console                        attach the terminal to the guest console
--no-persist / --reset           ephemeral state / recreate the overlay
--cache-dir / --data-dir PATH    override the default .cache / .state locations
--config PATH                    load a YAML config file (flags override it)
--debug                          print the full QEMU command and diagnostics
```

## Networking

The default backend is QEMU **user-mode networking (libslirp)** - unprivileged
and identical on Windows/macOS/Linux. Guest ports are exposed via `hostfwd`
bound to `127.0.0.1`. Outbound from the guest works through NAT (so `apt`,
`docker pull`, etc. work whenever the host has internet); inbound is only via the
forwards you declare. (Raw `ping` may not work even when TCP does - test with
`curl`/TCP.)

**Guest IP configuration** (`--ip-config`):

- `cloud-init` (default) - DHCP via the generated NoCloud seed (best for cloudimg).
- `kernel-dhcp` - `ip=dhcp` on the kernel cmdline (needs `CONFIG_IP_PNP_DHCP`).
- `kernel-static` - a deterministic static SLIRP address on the kernel cmdline.
- `none` - the image configures networking itself.

For port forwards to reach a service, the service must listen on `0.0.0.0`
inside the guest, not `127.0.0.1`.

## Building

```sh
go build -o dist/dvm ./cmd/dvm                                  # host

# macOS (Apple Silicon is the primary target; amd64 kept for older Intel Macs)
build/macos/build-dvm.sh                                        # darwin/arm64 + vz (cgo + codesign)
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="clang -arch x86_64" \
  go build -o dist/dvm-darwin-amd64 ./cmd/dvm                   # darwin/amd64 (Intel)

# Linux + Windows (pure Go, cross-compile from anywhere)
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o dist/dvm-linux-arm64      ./cmd/dvm
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/dvm-linux-amd64      ./cmd/dvm
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/dvm-windows-amd64.exe ./cmd/dvm

go test ./...
```

Releases are produced by [`.github/workflows/release.yml`](.github/workflows/release.yml):
on a `vX.Y.Z` tag it builds every `dvm` binary and the static QEMU for each
platform, then attaches them to the GitHub Release. The static QEMU build harness
lives in [`build/qemu/`](build/qemu/).

## Project layout

```
cmd/dvm/                 CLI entry + subcommand dispatch
internal/app/            config, flag merge, orchestration, version
internal/component/      ref parsing + provider interface (OCI now, local fallback)
internal/oci/            oras-go puller (multi-arch index select, layer select, digest verify)
internal/cache/          content-addressed blob store
internal/qemu/           command builder, network backend, accel, QMP client, QEMU auto-download
internal/vm/             overlay, cloud-init seed, console, readiness, lifecycle (qemu)
internal/vz/             native macOS engine (Virtualization.framework, cgo)
internal/platform/       cache/state dirs, host/guest arch
build/qemu/              static QEMU build harness (macOS/Linux/Windows)
build/macos/             cgo build + codesign for the vz engine
examples/                example config (examples/dvm.yaml)
```
