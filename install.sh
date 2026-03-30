#!/usr/bin/env bash

# ktsu — universal installation script (Stealth Mode Compatible)
# https://github.com/kimitsu-ai/ktsu

set -e

REPO="kimitsu-ai/ktsu"
BINARY_NAME="ktsu"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
GH_TOKEN="${GH_TOKEN:-$GITHUB_TOKEN}"

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

VERSION=""

# Method 1: Use 'gh' CLI if available and authenticated
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    echo "=> Using 'gh' CLI for authentication..."
    VERSION=$(gh release list --repo "$REPO" --limit 1 | awk '{print $1}')
fi

# Method 2: Use CURL with Token if provided
if [ -z "$VERSION" ] && [ -n "$GH_TOKEN" ]; then
    echo "=> Using GITHUB_TOKEN for authentication..."
    LATEST_RELEASE=$(curl -s -H "Authorization: token $GH_TOKEN" "https://api.github.com/repos/$REPO/releases/latest")
    VERSION=$(echo "$LATEST_RELEASE" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
fi

# Method 3: Anonymous Fallback (Public Only)
if [ -z "$VERSION" ]; then
    echo "=> Attempting anonymous fetch (for public repos)..."
    LATEST_RELEASE=$(curl -s "https://api.github.com/repos/$REPO/releases/latest")
    VERSION=$(echo "$LATEST_RELEASE" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
fi

if [ -z "$VERSION" ] || [ "$VERSION" = "null" ]; then
    echo "Error: Could not find latest release for $REPO. If this is a private repo, ensure you have 'gh' logged in or 'GITHUB_TOKEN' exported."
    exit 1
fi

echo "=> Version: $VERSION"
echo "=> Platform: $OS/$ARCH"

TMP_DIR=$(mktemp -d)
ASSET_NAME="${BINARY_NAME}_${VERSION#v}_${OS}_${ARCH}.tar.gz"

# Download Logic
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    gh release download "$VERSION" --repo "$REPO" --pattern "$ASSET_NAME" --dir "$TMP_DIR"
elif [ -n "$GH_TOKEN" ]; then
    # Two-step download for private assets via API
    # Extracts the "id" that precedes the matching "name" in the JSON response
    ASSET_ID=$(echo "$LATEST_RELEASE" | sed -n '/"id":/{h;}; /"name": "'"$ASSET_NAME"'"/{g;p;}' | head -n 1 | sed -E 's/.*"id": ([0-9]+).*/\1/')
    if [ -z "$ASSET_ID" ]; then
        echo "Error: Could not find asset $ASSET_NAME in release $VERSION"
        exit 1
    fi
    curl -sSL -H "Authorization: token $GH_TOKEN" \
         -H "Accept: application/octet-stream" \
         "https://api.github.com/repos/$REPO/releases/assets/$ASSET_ID" \
         -o "$TMP_DIR/$ASSET_NAME"
else
    DOWNLOAD_URL="https://github.com/kimitsu-ai/ktsu/releases/download/$VERSION/$ASSET_NAME"
    echo "=> Downloading from $DOWNLOAD_URL..."
    curl -sSL "$DOWNLOAD_URL" -o "$TMP_DIR/$ASSET_NAME"
fi

echo "=> Extracting..."
tar -xzf "$TMP_DIR/$ASSET_NAME" -C "$TMP_DIR"

echo "=> Installing to $INSTALL_DIR/$BINARY_NAME..."
mkdir -p "$INSTALL_DIR"
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
