.PHONY: build build-cli clean test fmt lint install uninstall

# Build all
all: build build-cli

# Build manager
build:
	CGO_ENABLED=1 go build -o warp-proxy-manager ./cmd/manager/

# Build CLI
build-cli:
	go build -o warpctl ./cmd/cli/

# Build for release (cross-compile)
release:
	GOOS=linux GOARCH=amd64 go build -o warpctl-linux-amd64 ./cmd/cli/
	GOOS=linux GOARCH=arm64 go build -o warpctl-linux-arm64 ./cmd/cli/
	GOOS=darwin GOARCH=amd64 go build -o warpctl-darwin-amd64 ./cmd/cli/
	GOOS=darwin GOARCH=arm64 go build -o warpctl-darwin-arm64 ./cmd/cli/

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
	@echo "✓ Installed warpctl to /usr/local/bin/warpctl"

# Uninstall CLI
uninstall:
	sudo rm -f /usr/local/bin/warpctl
	@echo "✓ Removed warpctl from /usr/local/bin"

# Quick install (curl one-liner)
quick-install:
	@echo "curl -sSL https://raw.githubusercontent.com/lowkruc/warp-proxy-manager/main/install.sh | bash"
