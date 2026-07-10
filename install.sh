#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Config
REPO="lowkruc/warp-proxy-manager"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="warpctl"

print_banner() {
    echo -e "${BLUE}"
    echo " ╦╔╗╔╔╦╗╔═╗╦═╗╔╦╗╔═╗╔═╗╔═╗ ╔═╗╔═╗╔═╗╔═╗╔╗ ╔═╗╔╦╗"
    echo " ║║║║ ║ ║╣ ╠╦╝║║║╠═╣║  ╚═╗ ╠═╣║  ║ ║║╣ ╠╩╗║ ║ ║ ║"
    echo " ╩╝╚╝ ╩ ╚═╝╩╚═╩ ╩╩ ╩╚═╝╚═╝ ╩ ╩╚═╝╚═╝╚═╝╚═╝╚═╝ ╩ "
    echo -e "${NC}"
    echo -e "${GREEN}  Installer${NC}"
}

check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"
    
    # Check Docker
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}Error: Docker not found${NC}"
        echo "Install Docker: https://docs.docker.com/engine/install/"
        exit 1
    fi
    echo -e "${GREEN}✓ Docker found${NC}"
    
    # Check Docker Compose
    if ! docker compose version &> /dev/null; then
        echo -e "${RED}Error: Docker Compose not found${NC}"
        echo "Install Docker Compose: https://docs.docker.com/compose/install/"
        exit 1
    fi
    echo -e "${GREEN}✓ Docker Compose found${NC}"
    
    # Check architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)  ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        armv7l)  ARCH="armv7" ;;
        *)
            echo -e "${RED}Error: Unsupported architecture: $ARCH${NC}"
            exit 1
            ;;
    esac
    echo -e "${GREEN}✓ Architecture: $ARCH${NC}"
}

detect_os() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case $OS in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)
            echo -e "${RED}Error: Unsupported OS: $OS${NC}"
            exit 1
            ;;
    esac
}

download_binary() {
    echo -e "\n${YELLOW}Downloading warpctl...${NC}"
    
    # Get latest release
    LATEST=$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d '"' -f 4)
    
    if [ -z "$LATEST" ]; then
        echo -e "${RED}Error: Could not fetch latest release${NC}"
        echo "Falling back to main branch..."
        LATEST="main"
    fi
    
    # Construct download URL
    BINARY_URL="https://github.com/$REPO/releases/download/$LATEST/warpctl-${OS}-${ARCH}"
    
    if [ "$LATEST" = "main" ]; then
        BINARY_URL="https://github.com/$REPO/raw/main/warpctl-${OS}-${ARCH}"
    fi
    
    # Download
    HTTP_CODE=$(curl -sSL -w "%{http_code}" -o /tmp/warpctl "$BINARY_URL")
    
    if [ "$HTTP_CODE" != "200" ]; then
        echo -e "${RED}Error: Download failed (HTTP $HTTP_CODE)${NC}"
        echo "URL: $BINARY_URL"
        exit 1
    fi
    
    chmod +x /tmp/warpctl
    
    # Move to install dir (needs sudo)
    echo -e "${YELLOW}Installing to ${INSTALL_DIR}/${BINARY_NAME}...${NC}"
    
    if [ -w "$INSTALL_DIR" ]; then
        mv /tmp/warpctl "${INSTALL_DIR}/${BINARY_NAME}"
    else
        sudo mv /tmp/warpctl "${INSTALL_DIR}/${BINARY_NAME}"
    fi
    
    echo -e "${GREEN}✓ Installed warpctl to ${INSTALL_DIR}/${BINARY_NAME}${NC}"
}

run_init() {
    echo -e "\n${YELLOW}Running setup...${NC}\n"
    warpctl init
}

main() {
    print_banner
    check_prerequisites
    detect_os
    download_binary
    run_init
    
    echo -e "\n${GREEN}================================${NC}"
    echo -e "${GREEN}  Installation complete!${NC}"
    echo -e "${GREEN}================================${NC}"
    echo ""
    echo "Quick start:"
    echo "  warpctl start      Start the manager"
    echo "  warpctl status     Check status"
    echo "  warpctl help       Show all commands"
    echo ""
    echo "SOCKS5 proxy: localhost:1080"
    echo "API:          http://localhost:8080"
    echo ""
}

main "$@"
