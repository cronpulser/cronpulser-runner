#!/bin/sh
set -eu

VERSION="${VERSION:-dev}"
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST_DIR="$PROJECT_DIR/dist"

mkdir -p "$DIST_DIR"
cd "$PROJECT_DIR"

for ARCH in amd64 arm64; do
  OUTPUT="$DIST_DIR/cronpulser-runner-linux-$ARCH"
  echo "Building $OUTPUT"
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build \
    -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o "$OUTPUT" \
    ./cmd/cronpulser-runner
done

OUTPUT="$DIST_DIR/cronpulser-runner-windows-amd64.exe"
echo "Building $OUTPUT"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$OUTPUT" \
  ./cmd/cronpulser-runner

echo "Binaries written to $DIST_DIR"
