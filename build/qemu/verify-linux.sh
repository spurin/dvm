#!/usr/bin/env bash
#
# verify-linux.sh — verify a staged static Linux QEMU build. Runs the checks
# inside an Alpine container matching the binary's arch (so it works from macOS
# or Linux). Asserts the binary is fully static and that KVM + libslirp are
# compiled in. Does not require /dev/kvm (no guest boot).
#
#   ./verify-linux.sh <stage-dir> <qemu-system-binary-name>
#   ./verify-linux.sh stage-linux-arm64/stage qemu-system-aarch64
#
set -euo pipefail
STAGE="${1:?usage: verify-linux.sh <stage-dir> <qemu-binary>}"
QBIN="${2:?usage: verify-linux.sh <stage-dir> <qemu-binary>}"
STAGE_ABS="$(cd "$STAGE" && pwd)"

case "$QBIN" in
  *x86_64)  PLAT=linux/amd64; MACHINE=q35 ;;
  *aarch64) PLAT=linux/arm64; MACHINE=virt ;;
  *) echo "unknown arch for $QBIN" >&2; exit 1 ;;
esac

docker run --rm -i --platform "$PLAT" -v "$STAGE_ABS:/stage:ro" \
  -e QBIN="$QBIN" -e MACHINE="$MACHINE" alpine:3.21 sh -s <<'EOF'
set -e
apk add --no-cache file >/dev/null 2>&1
f="/stage/bin/$QBIN"
[ -x "$f" ] || { echo "FAIL: missing $f"; exit 1; }

echo "== 1. fully static =="
file "$f"
file "$f" | grep -qE "static-pie linked|statically linked" || { echo "FAIL: not static"; exit 1; }
if readelf -l "$f" 2>/dev/null | grep -qi interp; then echo "FAIL: has INTERP (dynamic)"; exit 1; fi
echo "OK: no dynamic interpreter"

echo "== 2. version =="
"$f" --version | head -1
/stage/bin/qemu-img --version | head -1

echo "== 3. KVM compiled in =="
"$f" -accel help 2>&1 | grep -qw kvm || { echo "FAIL: kvm not compiled in"; exit 1; }
echo "OK: kvm present"

echo "== 4. libslirp (user networking) compiled in =="
"$f" -machine "$MACHINE" -accel tcg -netdev user,id=n0 -nographic -S -monitor none -serial none 2>/tmp/s &
pid=$!; sleep 1; kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true
grep -qi "network backend 'user' is not compiled" /tmp/s && { echo "FAIL: slirp missing"; exit 1; }
echo "OK: user-mode networking available"

echo
echo "ALL CHECKS PASSED"
EOF
