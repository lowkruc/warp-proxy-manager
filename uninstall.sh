#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

INSTALL_DIR="/opt/warp-proxy-manager"
BINARY="/usr/local/bin/warpctl"

echo -e "${YELLOW}=== Warp Proxy Manager Uninstall ===${NC}"
echo ""

# Check if warpctl exists
if command -v warpctl &> /dev/null; then
    echo "Using warpctl uninstall..."
    warpctl uninstall
    exit 0
fi

# Manual uninstall
echo "Performing manual uninstall..."

# Stop containers
if [ -f "${INSTALL_DIR}/docker-compose.yml" ]; then
    echo "Stopping containers..."
    docker compose -f "${INSTALL_DIR}/docker-compose.yml" down -v 2>/dev/null || true
fi

# Remove warp-proxy containers
echo "Removing warp-proxy containers..."
docker ps -a --filter "label=warp-proxy-managed=true" -q | xargs -r docker rm -f 2>/dev/null || true

# Remove install directory
if [ -d "$INSTALL_DIR" ]; then
    echo "Removing ${INSTALL_DIR}..."
    rm -rf "$INSTALL_DIR"
fi

# Remove binary
if [ -f "$BINARY" ]; then
    echo "Removing warpctl binary..."
    rm -f "$BINARY"
fi

echo ""
echo -e "${GREEN}✓ Uninstalled${NC}"
echo "  Docker images not removed (run 'docker image prune' to clean)"
