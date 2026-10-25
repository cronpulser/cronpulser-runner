#!/bin/sh
set -eu

# Releases are built by GitHub Actions and published as GitHub Releases. A
# custom base URL uses the layout {base}/{version}/{file}.
GITHUB_RELEASES_URL="https://github.com/cronpulser/cronpulser-runner/releases/download"
DOWNLOAD_BASE_URL="${CRONPULSER_DOWNLOAD_BASE_URL:-}"
VERSION="${CRONPULSER_RUNNER_VERSION:-0.3.5}"
BINARY_PATH=""
SERVER=""
TOKEN=""
UPGRADE=0

usage() {
  echo "Usage: install-runner.sh --server URL --token TOKEN [--version VERSION] [--download-base-url URL] [--binary-path PATH]" >&2
  echo "       install-runner.sh --upgrade [--version VERSION] [--download-base-url URL] [--binary-path PATH]" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server) SERVER="${2:-}"; shift 2 ;;
    --token) TOKEN="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    --download-base-url) DOWNLOAD_BASE_URL="${2:-}"; shift 2 ;;
    --binary-path) BINARY_PATH="${2:-}"; shift 2 ;;
    --upgrade) UPGRADE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [ "$UPGRADE" = 1 ]; then
  if [ -n "$SERVER" ] || [ -n "$TOKEN" ]; then
    echo "--upgrade keeps the existing registration; do not pass --server or --token." >&2
    exit 2
  fi
elif [ -z "$SERVER" ] || [ -z "$TOKEN" ]; then
  usage
  exit 2
fi

case "$VERSION" in
  ''|*[!0-9A-Za-z._-]*) echo "Invalid version: $VERSION" >&2; exit 2 ;;
esac

if [ -n "$DOWNLOAD_BASE_URL" ]; then
  RELEASE_URL="${DOWNLOAD_BASE_URL%/}/$VERSION"
else
  RELEASE_URL="$GITHUB_RELEASES_URL/v$VERSION"
fi

if [ "$(uname -s)" != "Linux" ]; then
  echo "CronPulser Runner currently supports Linux only." >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

TEMP_DIR=""
cleanup() {
  if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then rm -rf "$TEMP_DIR"; fi
}
trap cleanup EXIT INT TERM

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "SHA-256 verification requires sha256sum or shasum." >&2
    exit 1
  fi
}

if [ -n "$BINARY_PATH" ]; then
  if [ ! -x "$BINARY_PATH" ]; then echo "--binary-path must point to an executable file" >&2; exit 1; fi
  RUNNER_BINARY="$BINARY_PATH"
else
  TEMP_DIR=$(mktemp -d)
  RUNNER_BINARY="$TEMP_DIR/cronpulser-runner"
  FILE_NAME="cronpulser-runner-linux-$ARCH"
  URL="$RELEASE_URL/$FILE_NAME"
  CHECKSUM_URL="$RELEASE_URL/SHA256SUMS"
  CHECKSUM_FILE="$TEMP_DIR/SHA256SUMS"
  echo "Downloading $URL"
  curl -fsSL "$URL" -o "$RUNNER_BINARY"
  curl -fsSL "$CHECKSUM_URL" -o "$CHECKSUM_FILE"

  EXPECTED_SHA256=$(awk -v file="$FILE_NAME" '$2 == file { print $1; exit }' "$CHECKSUM_FILE")
  case "$EXPECTED_SHA256" in
    ''|*[!0-9a-fA-F]*)
      echo "No valid SHA-256 checksum found for $FILE_NAME." >&2
      exit 1
      ;;
  esac
  if [ "${#EXPECTED_SHA256}" -ne 64 ]; then
    echo "Invalid SHA-256 checksum length for $FILE_NAME." >&2
    exit 1
  fi

  ACTUAL_SHA256=$(sha256_file "$RUNNER_BINARY")
  if [ "$ACTUAL_SHA256" != "$EXPECTED_SHA256" ]; then
    echo "SHA-256 verification failed for $FILE_NAME." >&2
    exit 1
  fi
  echo "Verified SHA-256: $ACTUAL_SHA256"
  chmod 0755 "$RUNNER_BINARY"
fi

if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi
if [ "$UPGRADE" = 1 ]; then
  $SUDO "$RUNNER_BINARY" upgrade
else
  $SUDO "$RUNNER_BINARY" install --server "$SERVER" --token "$TOKEN"
fi
