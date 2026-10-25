#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "Usage: package-release.sh VERSION OUTPUT_ROOT" >&2
  exit 2
fi

VERSION=$1
OUTPUT_ROOT=$2
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
RELEASE_ROOT="$OUTPUT_ROOT/runner"
VERSION_DIR="$RELEASE_ROOT/$VERSION"

case "$VERSION" in
  ''|*[!0-9A-Za-z._-]*)
    echo "Version may contain only letters, numbers, dots, underscores, and hyphens." >&2
    exit 2
    ;;
esac

if [ -e "$VERSION_DIR" ]; then
  echo "Refusing to replace immutable release directory: $VERSION_DIR" >&2
  exit 1
fi

for ARCH in amd64 arm64; do
  BINARY="$PROJECT_DIR/dist/cronpulser-runner-linux-$ARCH"
  if [ ! -f "$BINARY" ]; then
    echo "Missing $BINARY; run VERSION=$VERSION ./scripts/build.sh first." >&2
    exit 1
  fi
done
WINDOWS_BINARY="$PROJECT_DIR/dist/cronpulser-runner-windows-amd64.exe"
if [ ! -f "$WINDOWS_BINARY" ]; then
  echo "Missing $WINDOWS_BINARY; run VERSION=$VERSION ./scripts/build.sh first." >&2
  exit 1
fi

mkdir -p "$VERSION_DIR"
for ARCH in amd64 arm64; do
  cp "$PROJECT_DIR/dist/cronpulser-runner-linux-$ARCH" "$VERSION_DIR/"
done
cp "$WINDOWS_BINARY" "$VERSION_DIR/"
cp "$PROJECT_DIR/install-runner.sh" "$RELEASE_ROOT/install-runner.sh"
cp "$PROJECT_DIR/install-runner.ps1" "$RELEASE_ROOT/install-runner.ps1"

(
  cd "$VERSION_DIR"
  sha256sum cronpulser-runner-linux-amd64 cronpulser-runner-linux-arm64 cronpulser-runner-windows-amd64.exe > SHA256SUMS
)

echo "Packaged immutable runner release $VERSION in $VERSION_DIR"
