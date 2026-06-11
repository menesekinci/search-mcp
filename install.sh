#!/usr/bin/env bash
# search-mcp Linux/macOS Installer
# One-liner: curl -fsSL https://.../install.sh | bash
set -euo pipefail

BOLD="\033[1m"
GREEN="\033[32m"
YELLOW="\033[33m"
CYAN="\033[36m"
RED="\033[31m"
RESET="\033[0m"

VERSION="${VERSION:-v0.5.1}"
REPO="menesekinci/search-mcp"
INSTALL_DIR="$HOME/.search-mcp"

# Detect OS/arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
esac

if [ "$OS" = "darwin" ]; then
    BINARY_NAME="search-mcp-darwin-${ARCH}"
elif [ "$OS" = "linux" ]; then
    BINARY_NAME="search-mcp-linux-${ARCH}"
else
    echo -e "${RED}Unsupported OS: $OS${RESET}"
    exit 1
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY_NAME}"

clear
echo -e "${CYAN}${BOLD}╔══════════════════════════════════════════════════════╗${RESET}"
echo -e "${CYAN}${BOLD}║     search-mcp — Linux/macOS Installer              ║${RESET}"
echo -e "${CYAN}${BOLD}║     ${VERSION}                                          ║${RESET}"
echo -e "${CYAN}${BOLD}╚══════════════════════════════════════════════════════╝${RESET}"
echo ""

# Step 1: Check prerequisites
echo -e "${BOLD}📌 Step 1/4: Checking prerequisites${RESET}"
echo ""

if command -v google-chrome &>/dev/null || command -v chromium &>/dev/null || [ -d "/Applications/Google Chrome.app" ]; then
    echo -e "  ${GREEN}✅ Chrome/Chromium found${RESET}"
else
    echo -e "  ${YELLOW}⚠️  Chrome not found — install from https://www.google.com/chrome/${RESET}"
fi

if command -v curl &>/dev/null; then
    echo -e "  ${GREEN}✅ curl available${RESET}"
else
    echo -e "  ${RED}❌ curl required${RESET}"
    exit 1
fi

echo ""

# Step 2: Download binary
echo -e "${BOLD}📌 Step 2/4: Installing search-mcp${RESET}"
echo ""

mkdir -p "$INSTALL_DIR"

echo -e "  📥 Downloading: $DOWNLOAD_URL"
if curl -fsSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/search-mcp"; then
    chmod +x "$INSTALL_DIR/search-mcp"
    SIZE=$(du -h "$INSTALL_DIR/search-mcp" | cut -f1)
    echo -e "  ${GREEN}✅ Installed: $INSTALL_DIR/search-mcp ($SIZE)${RESET}"
else
    echo -e "  ${YELLOW}⚠️  Download failed. Build from source: go install github.com/${REPO}@latest${RESET}"
    echo -e "  (requires Go 1.21+)"
    exit 1
fi

# Add to PATH
SHELL_CONFIG=""
if [ -f "$HOME/.zshrc" ]; then SHELL_CONFIG="$HOME/.zshrc"; 
elif [ -f "$HOME/.bashrc" ]; then SHELL_CONFIG="$HOME/.bashrc"; fi

if [ -n "$SHELL_CONFIG" ] && ! grep -q "$INSTALL_DIR" "$SHELL_CONFIG" 2>/dev/null; then
    echo "export PATH=\"\$PATH:$INSTALL_DIR\"" >> "$SHELL_CONFIG"
    echo -e "  ${GREEN}✅ Added to PATH in $SHELL_CONFIG${RESET}"
fi
export PATH="$PATH:$INSTALL_DIR"

echo ""

# Step 3: Kimi WebBridge check
echo -e "${BOLD}📌 Step 3/4: Checking Kimi WebBridge${RESET}"
echo ""

KIMI_DAEMON=""
for candidate in \
    "$HOME/.kimi-webbridge/bin/kimi-webbridge" \
    "/opt/kimi-webbridge/bin/kimi-webbridge" \
    "/usr/local/bin/kimi-webbridge"; do
    if [ -x "$candidate" ]; then
        KIMI_DAEMON="$candidate"
        break
    fi
done

if [ -n "$KIMI_DAEMON" ]; then
    echo -e "  ${GREEN}✅ Kimi daemon: $KIMI_DAEMON${RESET}"
else
    echo -e "  ${YELLOW}⚠️  Kimi WebBridge not found.${RESET}"
    echo "  Install from: https://kimi.moonshot.cn"
    echo "  Then re-run: search-mcp setup"
fi

echo ""

# Step 4: Setup wizard
echo -e "${BOLD}📌 Step 4/4: Running setup wizard${RESET}"
echo ""
"$INSTALL_DIR/search-mcp" setup

echo ""
echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════╗${RESET}"
echo -e "${GREEN}${BOLD}║              Installation Complete! 🎉              ║${RESET}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════╝${RESET}"
echo ""
echo "  Try asking your agent: 'search for latest AI papers'"
echo ""
