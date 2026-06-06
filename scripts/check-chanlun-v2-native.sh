#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
LOCAL_LIB="${ROOT_DIR}/chanlun_v2/target/release/libchanlun_v2.a"
SYSTEM_LIB="/usr/local/lib/libchanlun_v2.a"

echo "Chanlun V2 native dependency check"
echo "repo: ${ROOT_DIR}"

if command -v cargo >/dev/null 2>&1; then
  echo "cargo: $(cargo --version)"
else
  echo "cargo: missing"
fi

if [[ -f "${LOCAL_LIB}" ]]; then
  echo "local lib: found ${LOCAL_LIB}"
else
  echo "local lib: missing ${LOCAL_LIB}"
fi

if [[ -f "${SYSTEM_LIB}" ]]; then
  echo "system lib: found ${SYSTEM_LIB}"
else
  echo "system lib: missing ${SYSTEM_LIB}"
fi

echo "CGO_ENABLED: ${CGO_ENABLED:-default}"
echo
echo "Go-only fallback test:"
echo "  CGO_ENABLED=0 go test ./strategy/chanlunv2"
echo
echo "Native preparation:"
echo "  make native-chanlunv2"
echo
echo "Native test after local build:"
echo "  CGO_LDFLAGS=\"-L${ROOT_DIR}/chanlun_v2/target/release\" go test ./strategy/chanlunv2"
