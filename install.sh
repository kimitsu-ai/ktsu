#!/usr/bin/env bash

# ktsu — universal installation script
# https://github.com/kimitsu-ai/ktsu

set -e

REPO="kimitsu-ai/ktsu"
BINARY_NAME="ktsu"
INSTALL_DIR="/usr/local/bin"

# Detect OS and Architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Error: Unsupported architecture $ARCH"; exit 1 ;;
esac

case "$OS" in
    darwin|linux) ;;
    *) echo "Error: Unsupported operating system $OS"; exit 1 ;;
esac

echo "=> Detecting latest version..."
LATEST_RELEASE=$(curl -s https://api.github.com/repos/$REPO/releases/latest)
VERSION=$(echo "$LATEST_RELEASE" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$VERSION" ]; then
    echo "Error: Could not find latest release for $REPO"
    exit 1
fi

echo "=> Version: $VERSION"
echo "=> Platform: $OS/$ARCH"

# Match GoReleaser naming: ktsu_0.1.0_darwin_arm64.tar.gz
ASSET_NAME="${BINARY_NAME}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/kimitsu-ai/ktsu/releases/download/$VERSION/$ASSET_NAME"

echo "=> Downloading from $DOWNLOAD_URL..."
TMP_DIR=$(mktemp -d)
curl -sSL "$DOWNLOAD_URL" -o "$TMP_DIR/$ASSET_NAME"

echo "=> Extracting..."
tar -xzf "$TMP_DIR/$ASSET_NAME" -C "$TMP_DIR"

echo "=> Installing to $INSTALL_DIR/$BINARY_NAME..."
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
else
    echo "=> Sudo required to move binary into $INSTALL_DIR"
    sudo mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
fi

chmod +x "$INSTALL_DIR/$BINARY_NAME"
rm -rf "$TMP_DIR"

echo ""
echo "Successfully installed $BINARY_NAME $VERSION to $INSTALL_DIR"
echo "Try running: $BINARY_NAME --help"
