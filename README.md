<div align="center">

# 🌐 Warp Proxy Manager

**Dynamic load balancer & auto-scaler for Cloudflare WARP tunnels**

[![CI](https://github.com/lowkruc/warp-proxy-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/lowkruc/warp-proxy-manager/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lowkruc/warp-proxy-manager?color=blue&label=latest)](https://github.com/lowkruc/warp-proxy-manager/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/lowkruc/warp-proxy-manager?color=00ADD8)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Flowkruc%2Fwarp--proxy--manager-blue?logo=docker)](https://ghcr.io/lowkruc/warp-proxy-manager)

<br />

[Features](#-features) · [Quick Start](#-quick-start) · [Architecture](#-architecture) · [Configuration](#-configuration) · [API](#-api) · [Contributing](#-contributing) · [Support](#-support)

---

</div>

## ✨ Features

| Feature | Description |
|---------|-------------|
| 🔄 **Auto-Scaling** | Scale containers based on connections or 429/5xx response codes |
| 🔀 **Load Balancing** | Round-robin, least connections, or IP hash algorithms |
| 🩺 **Health Checking** | Automatic backend health monitoring & failover |
| 🧦 **SOCKS5 Proxy** | Forward proxy with optional user/password authentication |
| 📡 **REST API** | Full API with OpenAPI spec for programmatic control |
| 📊 **Prometheus Metrics** | Export metrics for monitoring dashboards |
| 🐳 **Docker Native** | Auto-create, manage & cleanup warp-proxy containers |
| 🏷️ **Label-Based Tracking** | Containers tracked by label, not prefix — no conflicts |
| 🔄 **Orphan Cleanup** | Auto-remove orphaned containers on startup & shutdown |
| ⚡ **Pure Go** | No CGO required — single static binary |

## 📦 Quick Start

### Docker Compose (recommended)

```bash
git clone https://github.com/lowkruc/warp-proxy-manager.git
cd warp-proxy-manager

# Edit config
cp config.example.yaml config.yaml
# ... edit config.yaml ...

# Run
docker compose up -d
```

### Verify

```bash
# Health check
curl http://localhost:8080/health

# Test SOCKS5
curl --socks5-hostname localhost:1080 https://ifconfig.me

# List containers
curl http://localhost:8080/api/v1/containers
```

## 🏗️ Architecture

```
┌──────────────┐
│    Client    │
└──────┬───────┘
       │ SOCKS5
       ▼
┌──────────────────────────────────────┐
│         Warp Proxy Manager           │
│  ┌─────────┐  ┌─────────┐           │
│  │  API    │  │ Proxy   │           │
│  │ :8080   │  │ :1080   │           │
│  └────┬────┘  └────┬────┘           │
│       │            │                 │
│  ┌────▼────────────▼────┐           │
│  │    Load Balancer     │           │
│  │ (round-robin/lc/ip)  │           │
│  └────┬────────────┬────┘           │
│       │            │                 │
│  ┌────▼────┐  ┌────▼────┐          │
│  │ Scaler  │  │ Health  │          │
│  │         │  │ Checker │          │
│  └─────────┘  └─────────┘          │
└──────────────────────────────────────┘
       │         │         │
       ▼         ▼         ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│  warp-1  │ │  warp-2  │ │  warp-N  │
│  WARP    │ │  WARP    │ │  WARP    │
│  :1081   │ │  :1082   │ │  :10XX   │
└──────────┘ └──────────┘ └──────────┘
```

Each warp-proxy container:
- Runs an independent Cloudflare WARP tunnel
- Rotates IP on `WARP_ROTATION_INTERVAL`
- Bounded by memory & CPU limits
- Auto-restarts on failure

## ⚙️ Configuration

```yaml
manager:
  api_port: 8080
  log_level: info

proxy:
  listen: ":1080"
  auth:
    enabled: false
    users:
      - user: admin
        pass: "$2a$10$..."  # bcrypt hash
  timeout:
    connect: 5s
    idle: 30s

scaling:
  min: 3
  max: 10
  cooldown: 60s
  triggers:
    - name: high_connections
      type: connection
      threshold: 100
      scale_direction: up
      scale_count: 2
      cooldown: 120s

    - name: low_connections
      type: connection
      threshold: 20
      scale_direction: down
      scale_count: 1
      cooldown: 300s

    - name: rate_limit
      type: response_code
      response_code: 429
      threshold: 10
      window: 60s
      scale_direction: up
      scale_count: 1
      cooldown: 120s

loadbalancer:
  algorithm: roundrobin
  health_check:
    enabled: true
    interval: 10s
    timeout: 5s
    unhealthy_threshold: 3

docker:
  image: "ghcr.io/lowkruc/warp-proxy:latest"
  network: "warp-net"
  prefix: "warp-proxy"
  memory_limit: "150m"
  cpu_limit: "0.1"
  env:
    WARP_SLEEP: "5"
    WARP_ROTATION_INTERVAL: "60"
```

> See [`config.example.yaml`](config.example.yaml) for full reference.

## 🔌 API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/api/v1/containers` | GET | List containers |
| `/api/v1/containers` | POST | Create container |
| `/api/v1/containers/:id` | DELETE | Remove container |
| `/api/v1/containers/:id/restart` | POST | Restart container |
| `/api/v1/proxy/stats` | GET | Proxy statistics |
| `/api/v1/proxy/connections` | GET | Active connections |
| `/api/v1/scaling` | GET/PUT | Scaling config |
| `/api/v1/scaling/scale/:n` | POST | Manual scale to N |
| `/api/v1/scaling/history` | GET | Scale events history |
| `/api/v1/metrics` | GET | Metrics (JSON) |
| `/metrics` | GET | Prometheus format |

> Full OpenAPI spec: [`api/openapi.yaml`](api/openapi.yaml)

## 🛠️ Development

```bash
# Build
go build -o warp-proxy-manager ./cmd/manager/
go build -o warpctl ./cmd/cli/

# Run tests
go test ./...

# Run locally
./warp-proxy-manager -config config.yaml

# Docker
docker compose up -d
```

## 🤝 Contributing

Contributions welcome! Please read our [contributing guidelines](CONTRIBUTING.md) first.

1. Fork the repo
2. Create your feature branch (`git checkout -b feature/amazing`)
3. Commit your changes (`git commit -m 'feat: add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing`)
5. Open a Pull Request

## 📜 License

MIT License — see [LICENSE](LICENSE) for details.

---

<div align="center">

## 💖 Support

If you find this project useful, consider supporting its development:

[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-ffdd00?style=for-the-badge&logo=buy-me-a-coffee&logoColor=black)](https://www.buymeacoffee.com/lowkruc)
[![GitHub Sponsors](https://img.shields.io/badge/GitHub%20Sponsors-ea4aaa?style=for-the-badge&logo=github-sponsors&logoColor=white)](https://github.com/sponsors/lowkruc)
[![Ko-fi](https://img.shields.io/badge/Ko--fi-FF5E5B?style=for-the-badge&logo=ko-fi&logoColor=white)](https://ko-fi.com/lowkruc)

<br />

**Made with ❤️ by [lowkruc](https://github.com/lowkruc)**

[⬆ Back to top](#-warp-proxy-manager)

</div>
