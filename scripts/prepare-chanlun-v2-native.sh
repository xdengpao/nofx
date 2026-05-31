#!/usr/bin/env bash
set -euo pipefail

INSTALL_LOCAL=false
for arg in "$@"; do
  case "${arg}" in
    --install-local)
      INSTALL_LOCAL=true
      ;;
    -h|--help)
      cat <<'USAGE'
Usage: scripts/prepare-chanlun-v2-native.sh [--install-local]

Builds chanlun_v2/target/release/libchanlun_v2.a for CGO tests.
Use --install-local only when you explicitly want to copy the library to /usr/local/lib.
USAGE
      exit 0
      ;;
    *)
      echo "unknown argument: ${arg}" >&2
      exit 2
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CRATE_DIR="${ROOT_DIR}/chanlun_v2"
LOCAL_LIB="${CRATE_DIR}/target/release/libchanlun_v2.a"
SYSTEM_LIB="/usr/local/lib/libchanlun_v2.a"

if ! command -v cargo >/dev/null 2>&1; then
  cat >&2 <<'EOF'
cargo not found. Install Rust toolchain first, or use the Docker backend build path.

Rustup install command:
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

After installing Rust:
  make native-chanlunv2
EOF
  exit 1
fi

echo "Building Chanlun V2 native library..."
(
  cd "${CRATE_DIR}"
  cargo build --release
)

if [[ ! -f "${LOCAL_LIB}" ]]; then
  echo "build finished but library not found: ${LOCAL_LIB}" >&2
  exit 1
fi

echo "built: ${LOCAL_LIB}"

if [[ "${INSTALL_LOCAL}" == "true" ]]; then
  if [[ -w "$(dirname "${SYSTEM_LIB}")" ]]; then
    cp "${LOCAL_LIB}" "${SYSTEM_LIB}"
    echo "installed: ${SYSTEM_LIB}"
  else
    cat >&2 <<EOF
Cannot write to $(dirname "${SYSTEM_LIB}").
Run this command manually if you want a system-wide install:
  sudo install -m 0644 "${LOCAL_LIB}" "${SYSTEM_LIB}"
EOF
    exit 1
  fi
fi

cat <<EOF

Native test command:
  CGO_LDFLAGS="-L${CRATE_DIR}/target/release" go test ./strategy/chanlunv2
EOF
