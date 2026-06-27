#!/usr/bin/env bash
#
# verify.sh — acceptance checks for the staged static QEMU. CI-safe: does NOT
# require HVF (hosted runners lack nested virt), so it never boots a guest. The
# HVF boot-parity test is run separately on a real Mac.
#
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$HERE/stage/bin/qemu-system-aarch64"
IMG="$HERE/stage/bin/qemu-img"

fail() { echo "FAIL: $*" >&2; exit 1; }

[ -x "$BIN" ] || fail "missing $BIN (run ./build-macos.sh first)"
[ -x "$IMG" ] || fail "missing $IMG"

echo "== 1. static-linkage acceptance: no Homebrew dylibs =="
otool -L "$BIN"
if otool -L "$BIN" | grep -q "/opt/homebrew"; then
  fail "binary references /opt/homebrew — not relocatable"
fi
# Every dynamic dependency must be an Apple-provided path.
if otool -L "$BIN" | tail -n +2 | grep -vqE '^[[:space:]]+(/usr/lib/|/System/Library/Frameworks/)'; then
  echo "WARNING: a non-Apple dynamic dependency is present:" >&2
  otool -L "$BIN" | tail -n +2 | grep -vE '/usr/lib/|/System/Library/Frameworks/' >&2 || true
  fail "unexpected non-system dynamic dependency"
fi
echo "OK: only /usr/lib + /System/Library/Frameworks"

echo "== 2. version =="
"$BIN" --version | head -1
"$IMG" --version | head -1

echo "== 3. HVF compiled in =="
"$BIN" -accel help 2>&1 | grep -qw hvf || fail "hvf not compiled in"
echo "OK: hvf present"

echo "== 4. libslirp (user networking) compiled in =="
log="$(mktemp)"
"$BIN" -machine virt -accel tcg -netdev user,id=n0 -nographic -S -monitor none -serial none 2>"$log" &
pid=$!; sleep 1; kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true
if grep -qi "network backend 'user' is not compiled" "$log"; then
  rm -f "$log"; fail "libslirp not compiled in"
fi
rm -f "$log"
echo "OK: user-mode networking available"

echo
echo "ALL CHECKS PASSED"
