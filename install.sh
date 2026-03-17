#!/bin/sh
set -e

REPO="buzzhpc/buzz-cli"
BINARY="buzz-cli"
INSTALL_DIR="/usr/local/bin"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest version
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$VERSION" ]; then
  echo "Failed to determine latest version"
  exit 1
fi

echo "Installing buzz ${VERSION} (${OS}/${ARCH})..."

URL="https://github.com/${REPO}/releases/download/${VERSION}/buzz_${VERSION#v}_${OS}_${ARCH}.tar.gz"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" | tar -xz -C "$TMP"
chmod 755 "$TMP/$BINARY"
mv "$TMP/$BINARY" "$INSTALL_DIR/buzz"

echo "buzz installed to $INSTALL_DIR/buzz"
"$INSTALL_DIR/buzz" --version
