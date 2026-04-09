#!/usr/bin/env bash
set -euo pipefail

# drizz-farm installer
# Usage: curl -fsSL https://get.drizz.dev/farm | bash

BINARY_NAME="drizz-farm"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="$HOME/.drizz-farm"
DOWNLOAD_BASE="https://dist.drizz.dev/v1"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $1"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Detect OS and architecture
detect_platform() {
    local os arch

    case "$(uname -s)" in
        Darwin) os="darwin" ;;
        Linux)  os="linux" ;;
        *)      error "Unsupported OS: $(uname -s). drizz-farm requires macOS or Linux." ;;
    esac

    case "$(uname -m)" in
        arm64|aarch64) arch="arm64" ;;
        x86_64|amd64)  arch="amd64" ;;
        *)             error "Unsupported architecture: $(uname -m)" ;;
    esac

    echo "${os}_${arch}"
}

# Check prerequisites
check_prereqs() {
    info "Checking prerequisites..."

    if [[ "$(uname -s)" == "Darwin" ]]; then
        # Check macOS version
        local macos_version
        macos_version=$(sw_vers -productVersion 2>/dev/null || echo "unknown")
        ok "macOS $macos_version"

        # Check for Apple Silicon
        if [[ "$(uname -m)" == "arm64" ]]; then
            ok "Apple Silicon detected"
        else
            warn "Intel Mac detected. Apple Silicon recommended for best performance."
        fi
    fi

    # Check for Android SDK (optional)
    if [[ -n "${ANDROID_HOME:-}" ]] && [[ -d "$ANDROID_HOME" ]]; then
        ok "Android SDK found: $ANDROID_HOME"
    elif [[ -d "$HOME/Library/Android/sdk" ]]; then
        ok "Android SDK found: $HOME/Library/Android/sdk"
    else
        warn "Android SDK not found. Run 'drizz-farm setup' after install to configure."
    fi
}

# Download and install binary
install_binary() {
    local platform="$1"
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    info "Downloading drizz-farm for $platform..."

    # For now, build from source if download fails
    # In production, this would download from dist.drizz.dev
    local download_url="${DOWNLOAD_BASE}/download/${BINARY_NAME}_${platform}.tar.gz"

    if command -v curl &>/dev/null; then
        if curl -fsSL "$download_url" -o "$tmp_dir/${BINARY_NAME}.tar.gz" 2>/dev/null; then
            tar -xzf "$tmp_dir/${BINARY_NAME}.tar.gz" -C "$tmp_dir"
        else
            warn "Download not available yet (pre-release). Please build from source:"
            echo ""
            echo "  git clone https://github.com/drizz-dev/drizz-farm.git"
            echo "  cd drizz-farm"
            echo "  make build"
            echo "  sudo cp bin/drizz-farm /usr/local/bin/"
            echo ""
            exit 0
        fi
    fi

    # Install binary
    info "Installing to $INSTALL_DIR/$BINARY_NAME..."
    if [[ -w "$INSTALL_DIR" ]]; then
        cp "$tmp_dir/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
    else
        sudo cp "$tmp_dir/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
    fi
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    ok "Binary installed: $INSTALL_DIR/$BINARY_NAME"
}

# Create config directory
setup_config() {
    info "Creating config directory..."
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$CONFIG_DIR/artifacts"
    mkdir -p "$CONFIG_DIR/snapshots"
    ok "Config directory: $CONFIG_DIR"
}

# Print next steps
print_next_steps() {
    echo ""
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}  drizz-farm installed successfully!${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "Next steps:"
    echo ""
    echo "  1. Run the setup wizard:"
    echo "     drizz-farm setup"
    echo ""
    echo "  2. Start the daemon:"
    echo "     drizz-farm start"
    echo ""
    echo "  3. Activate your license (optional):"
    echo "     drizz-farm activate DRIZZ-FARM-XXXX-XXXX-XXXX"
    echo ""
    echo "  4. Create your first session:"
    echo "     drizz-farm session create --profile pixel_7_api34"
    echo ""
    echo "Documentation: https://docs.drizz.dev/farm"
    echo ""
}

# Main
main() {
    echo ""
    echo "  drizz-farm installer"
    echo "  Self-hosted emulator pool manager"
    echo ""

    local platform
    platform=$(detect_platform)

    check_prereqs
    install_binary "$platform"
    setup_config
    print_next_steps
}

main "$@"
