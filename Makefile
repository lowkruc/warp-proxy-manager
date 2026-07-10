.PHONY: build build-cli clean test fmt lint install uninstall

# Get version from git tag, fallback to "dev"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

# Build all
all: build build-cli

# Build manager
build:
	CGO_ENABLED=1 go build $(LDFLAGS) -o warp-proxy-manager ./cmd/manager/

# Build CLI
build-cli:
	go build $(LDFLAGS) -o warpctl ./cmd/cli/

# Build for release (cross-compile)
release:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o warpctl-linux-amd64 ./cmd/cli/
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o warpctl-linux-arm64 ./cmd/cli/
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o warpctl-darwin-amd64 ./cmd/cli/
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o warpctl-darwin-arm64 ./cmd/cli/

# Clean
clean:
	rm -f warp-proxy-manager warpctl warpctl-*
	rm -rf data/

# Test
test:
	go test ./...

# Format
fmt:
	gofmt -w .

# Lint
lint:
	golangci-lint run

# Install CLI to /usr/local/bin
install: build-cli
	sudo cp warpctl /usr/local/bin/warpctl
	sudo chmod +x /usr/local/bin/warpctl
	@echo "✓ Installed warpctl $(VERSION) to /usr/local/bin/warpctl"

# Uninstall CLI
uninstall:
	sudo rm -f /usr/local/bin/warpctl
	@echo "✓ Removed warpctl from /usr/local/bin"

# Quick install (curl one-liner)
quick-install:
	@echo "curl -sSL https://raw.githubusercontent.com/lowkruc/warp-proxy-manager/main/install.sh | bash"
