#!/usr/bin/env bash
# drizz-farm install script
#
#   curl -fsSL https://get.drizz.ai | bash
#
# Downloads the latest macOS binary, places it at /usr/local/bin/drizz-farm,
# and kicks off 'drizz-farm setup'. Safe to re-run to upgrade.

set -euo pipefail

REPO="${DRIZZ_FARM_REPO:-parthadrizz/drizz-farm}"
INSTALL_DIR="${DRIZZ_FARM_INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="drizz-farm"

# ── Preflight ────────────────────────────────────────────────────────────
info() { printf "  \033[0;36m%s\033[0m\n" "$1"; }
ok()   { printf "  \033[0;32m✓\033[0m %s\n" "$1"; }
warn() { printf "  \033[0;33m⚠\033[0m %s\n" "$1"; }
fail() { printf "  \033[0;31m✗\033[0m %s\n" "$1" >&2; exit 1; }

OS=$(uname -s)
ARCH=$(uname -m)

if [[ "$OS" != "Darwin" ]]; then
  fail "drizz-farm currently supports macOS only (got $OS). Linux support is coming."
fi

case "$ARCH" in
  arm64|aarch64) ASSET_ARCH="arm64" ;;
  x86_64|amd64)  ASSET_ARCH="amd64" ;;
  *) fail "Unsupported architecture: $ARCH" ;;
esac

info "Platform: darwin/$ASSET_ARCH"

# ── Find latest release ──────────────────────────────────────────────────
LATEST_URL="https://api.github.com/repos/$REPO/releases/latest"
info "Finding latest release..."
TAG=$(curl -fsSL "$LATEST_URL" | grep '"tag_name"' | head -1 | cut -d'"' -f4 || true)

if [[ -z "$TAG" ]]; then
  fail "Could not find latest release. Check https://github.com/$REPO/releases"
fi
ok "Latest: $TAG"

# Prefer universal binary; fall back to arch-specific
ASSET="drizz-farm-$TAG-darwin-universal.tar.gz"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$TAG/$ASSET"

# ── Download ─────────────────────────────────────────────────────────────
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

info "Downloading $ASSET..."
if ! curl -fsSL -o "$TMPDIR/$ASSET" "$DOWNLOAD_URL"; then
  # fall back to arch-specific build
  ASSET="drizz-farm-$TAG-darwin-$ASSET_ARCH.tar.gz"
  DOWNLOAD_URL="https://github.com/$REPO/releases/download/$TAG/$ASSET"
  info "Universal binary not found, trying $ASSET..."
  curl -fsSL -o "$TMPDIR/$ASSET" "$DOWNLOAD_URL" || fail "Download failed"
fi
ok "Downloaded"

# ── Install ──────────────────────────────────────────────────────────────
info "Installing to $INSTALL_DIR/$BINARY_NAME..."
tar -xzf "$TMPDIR/$ASSET" -C "$TMPDIR"

# Find the extracted binary (name depends on whether it was universal or arch-specific)
EXTRACTED=$(find "$TMPDIR" -name "drizz-farm-darwin-*" -not -name "*.tar.gz" -type f | head -1)
[[ -z "$EXTRACTED" ]] && fail "Could not find binary in archive"

if [[ -w "$INSTALL_DIR" ]]; then
  mv "$EXTRACTED" "$INSTALL_DIR/$BINARY_NAME"
else
  warn "$INSTALL_DIR is not writable — using sudo"
  sudo mv "$EXTRACTED" "$INSTALL_DIR/$BINARY_NAME"
fi
chmod +x "$INSTALL_DIR/$BINARY_NAME" 2>/dev/null || sudo chmod +x "$INSTALL_DIR/$BINARY_NAME"
ok "Installed $BINARY_NAME $TAG"

# ── Next steps ───────────────────────────────────────────────────────────
echo
echo "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  drizz-farm is installed."
echo
echo "  Next:"
echo "    drizz-farm setup     # one-time: detect SDK, install as service"
echo "    drizz-farm start     # run it now (or: daemon install for auto-start)"
echo
echo "  Dashboard: http://\$(hostname).local:9401"
echo "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
