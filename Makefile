.PHONY: build clean run test

# Build
build:
	CGO_ENABLED=1 go build -o warp-proxy-manager ./cmd/manager/

# Clean
clean:
	rm -f warp-proxy-manager
	rm -rf data/

# Run
run: build
	./warp-proxy-manager -config config.yaml

# Run in background
run-bg: build
	./warp-proxy-manager -config config.yaml &

# Test
test:
	go test ./...

# Dev mode
dev:
	go run ./cmd/manager/ -config config.yaml

# Format
fmt:
	gofmt -w .

# Lint
lint:
	golangci-lint run

# Docker build
docker:
	docker build -t warp-proxy-manager .

# Create data dir
data:
	mkdir -p data
