# Contributing

Thanks for your interest in contributing to Warp Proxy Manager!

## Getting Started

1. Fork the repo
2. Clone your fork
3. Create a feature branch: `git checkout -b feature/my-feature`
4. Make your changes
5. Run tests: `go test ./...`
6. Commit: `git commit -m 'feat: add my feature'`
7. Push: `git push origin feature/my-feature`
8. Open a Pull Request

## Development Setup

```bash
# Prerequisites
# - Go 1.25+
# - Docker

# Build
go build -o warp-proxy-manager ./cmd/manager/
go build -o warpctl ./cmd/cli/

# Run tests
go test ./...

# Run locally
./warp-proxy-manager -config config.yaml
```

## Code Style

- Follow standard Go conventions
- Run `gofmt` before committing
- Keep functions focused and small
- Add comments for non-obvious logic

## Pull Requests

- Keep PRs focused on one change
- Include a clear description
- Add tests for new features
- Update documentation if needed

## Issues

- Use GitHub Issues for bugs and feature requests
- Include steps to reproduce for bugs
- Include expected vs actual behavior
