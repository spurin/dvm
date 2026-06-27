#!/usr/bin/env bash
#
# verify-windows.sh — verify a staged static Windows QEMU. The acceptance gate is
# a PE-import check (arch-independent, runs on any host via a container with the
# mingw objdump): every imported DLL must be a known Windows system DLL or a DLL
# bundled in the stage folder. Optional Wine run-checks (--accel/--version) are
# best on a native amd64 host / CI.
#
#   ./verify-windows.sh <stage-dir>
#   ./verify-windows.sh stage-windows-amd64/stage
#
set -euo pipefail
STAGE="${1:?usage: verify-windows.sh <stage-dir>}"
STAGE_ABS="$(cd "$STAGE" && pwd)"

docker run --rm -i -v "$STAGE_ABS:/stage:ro" fedora:41 bash -s <<'EOF'
set -e
dnf -y install mingw64-binutils >/dev/null 2>&1
exe=/stage/qemu-system-x86_64.exe
[ -f "$exe" ] || { echo "FAIL: missing $exe"; exit 1; }

echo "== imported DLLs =="
mapfile -t dlls < <(x86_64-w64-mingw32-objdump -p "$exe" | sed -n 's/.*DLL Name: //p' | tr -d '\r' | sort -u)
printf '  %s\n' "${dlls[@]}"

# Allowed Windows system DLLs (always present on Windows). Anything else must be
# bundled in the stage folder, else the .exe is not self-contained.
sys='^(kernel32|kernelbase|advapi32|ws2_32|iphlpapi|dnsapi|shell32|shlwapi|user32|winmm|ole32|oleaut32|msvcrt|ucrtbase|ntdll|bcrypt|crypt32|secur32|gdi32|version|userenv|setupapi|cfgmgr32|mswsock|psapi|powrprof|rpcrt4|comdlg32|imm32|dwmapi|winhvplatform|winhvemulation|api-ms-win-.*)\.dll$'

bad=0
for d in "${dlls[@]}"; do
  low="$(printf '%s' "$d" | tr 'A-Z' 'a-z')"
  if printf '%s' "$low" | grep -qE "$sys"; then continue; fi
  if [ -f "/stage/$d" ]; then echo "  (bundled) $d"; continue; fi
  echo "FAIL: non-system, non-bundled DLL dependency: $d"; bad=1
done
[ "$bad" -eq 0 ] || exit 1
echo "OK: self-contained (only Windows system DLLs imported)"

echo "== WHPX compiled in (string check) =="
# Cheap, host-independent: the accelerator name is present in the binary.
if grep -aq "whpx" "$exe"; then echo "OK: whpx present"; else echo "WARN: whpx string not found"; fi

echo
echo "ALL CHECKS PASSED (PE-import gate). Run-time --accel/--version checks: use Wine on native amd64 / CI."
EOF
