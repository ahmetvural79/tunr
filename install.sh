#!/bin/sh
set -e

REPO="ahmetvural79/tunr"
INSTALL_DIR="/usr/local/bin"
BINARY="tunr"

log() {
  printf "%s\n" "$1"
}

log "Step 1/7: Detecting platform..."

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin)  OS="darwin" ;;
  linux)   OS="linux" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)    ARCH="amd64" ;;
  arm64|aarch64)   ARCH="arm64" ;;
  i386|i686)       ARCH="i386" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

log "Step 2/7: Resolving latest release..."

TAG=$(curl -fsI "https://github.com/$REPO/releases/latest" | grep -i "^location:" | sed 's/.*tag\///' | tr -d '\r\n')
if [ -z "$TAG" ]; then
  echo "Failed to fetch latest version"
  exit 1
fi

VERSION="${TAG#v}"
FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"

case "$OS" in
  windows) FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}.zip" ;;
esac

URL="https://github.com/$REPO/releases/download/${TAG}/${FILENAME}"
CHECKSUM_URL="https://github.com/$REPO/releases/download/${TAG}/checksums.txt"

log "Installing tunr ${VERSION} (${OS}/${ARCH})..."

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

log "Step 3/7: Downloading archive..."
curl -fL --retry 3 --connect-timeout 15 --progress-bar "$URL" -o "$TMP/$FILENAME"

log "Step 4/7: Downloading checksums..."
curl -fsSL --retry 3 --connect-timeout 15 "$CHECKSUM_URL" -o "$TMP/checksums.txt"

log "Step 5/7: Verifying checksum..."
if [ "$OS" = "darwin" ]; then
  (cd "$TMP" && grep "$FILENAME" checksums.txt | shasum -a 256 -c --quiet)
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$TMP" && grep "$FILENAME" checksums.txt | sha256sum -c --quiet)
else
  echo "Warning: cannot verify checksum (sha256sum/shasum not found)"
fi

log "Step 6/7: Extracting archive..."
# Goreleaser puts binaries in subfolder: tunr_0.1.1_os_arch/
ARCHIVE_DIR="${BINARY}_${VERSION}_${OS}_${ARCH}"
if [ "$OS" = "windows" ]; then
  if ! command -v unzip >/dev/null 2>&1; then
    echo "Error: unzip is required for Windows. Install it or use Git Bash which includes unzip."
    exit 1
  fi
  unzip -o -q "$TMP/$FILENAME" -d "$TMP"
  BINFILE="$TMP/$ARCHIVE_DIR/${BINARY}.exe"
else
  tar -xzf "$TMP/$FILENAME" -C "$TMP"
  BINFILE="$TMP/$ARCHIVE_DIR/$BINARY"
fi

if [ "$OS" = "windows" ]; then
  INSTALL_DIR="/usr/local/bin"
  log "Step 7/7: Installing binary to $INSTALL_DIR..."
  mkdir -p "$INSTALL_DIR"
  cp "$BINFILE" "$INSTALL_DIR/${BINARY}.exe"
  chmod +x "$INSTALL_DIR/${BINARY}.exe"
elif [ -w "$INSTALL_DIR" ]; then
  log "Step 7/7: Installing binary to $INSTALL_DIR..."
  install -m 0755 "$BINFILE" "$INSTALL_DIR/$BINARY"
else
  log "Step 7/7: Installing binary to $INSTALL_DIR (sudo password may be required)..."
  sudo install -m 0755 "$BINFILE" "$INSTALL_DIR/$BINARY"
fi

if [ "$OS" = "windows" ]; then
  EXE="$INSTALL_DIR/${BINARY}.exe"
else
  EXE="$INSTALL_DIR/$BINARY"
fi

log ""
log "  tunr ${VERSION} installed to $EXE"
log ""
log "  Get started:"
log "    tunr share --port 3000"
log ""
"$EXE" version
