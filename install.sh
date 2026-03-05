#!/usr/bin/env bash
set -euo pipefail

REPO="glitchedgod/migrate-to-smartsheet"
BINARY="migrate-to-smartsheet"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  darwin) OS="darwin" ;;
  linux)  OS="linux" ;;
  *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Fetch latest release version
echo "Fetching latest release..."
VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name"' | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/')

if [ -z "$VERSION" ]; then
  echo "Error: could not determine latest version" >&2
  exit 1
fi

echo "Installing $BINARY v$VERSION ($OS/$ARCH)..."

FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/v${VERSION}/$FILENAME"

# Download and extract
TMP_DIR=$(mktemp -d)
curl -fsSL "$URL" | tar -xz -C "$TMP_DIR"
chmod +x "$TMP_DIR/$BINARY"

# Install to /usr/local/bin if writable, otherwise current directory
INSTALL_DIR="/usr/local/bin"
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_DIR/$BINARY" "$INSTALL_DIR/$BINARY"
  echo ""
  echo "✅ Installed to $INSTALL_DIR/$BINARY"
  echo ""
  echo "Run it from anywhere:"
  echo "  $BINARY"
else
  mv "$TMP_DIR/$BINARY" "./$BINARY"
  echo ""
  echo "✅ Installed: ./$BINARY"
  echo ""
  echo "To run from anywhere, move it to a directory on your PATH:"
  echo "  sudo mv ./$BINARY /usr/local/bin/$BINARY"
  echo ""
  echo "Or run directly:"
  echo "  ./$BINARY"
fi
rm -rf "$TMP_DIR"
