# Warp Proxy Manager

Dynamic load balancer & manager for warp-proxy containers.

```
┌─────────────────────────────────────────────────────────────────┐
│                    warp-proxy-manager                            │
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │  SOCKS5     │  │  REST API   │  │  Prometheus │            │
│  │  Proxy      │  │  (gin)      │  │  Metrics    │            │
│  │  :1080      │  │  :8080      │  │  /metrics   │            │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘            │
│         │                │                │                     │
│         └────────────────┼────────────────┘                     │
│                          │                                      │
│                    ┌─────▼─────┐                                │
│                    │  Core     │                                │
│                    └─────┬─────┘                                │
│                          │                                      │
│    ┌─────────────────────┼─────────────────────┐               │
│    │                     │                     │                │
│ ┌──▼──┐             ┌───▼───┐            ┌───▼───┐           │
│ │Load │             │Scaler │            │Health │            │
│ │Balnc│             │       │            │Check  │            │
│ └──┬──┘             └───┬───┘            └───┬───┘           │
│    │                    │                     │                │
│    └────────────────────┼─────────────────────┘               │
│                         │                                      │
└─────────────────────────┼──────────────────────────────────────┘
                          │
                   ┌──────▼──────┐
                   │   Docker    │
                   └──────┬──────┘
                          │
    ┌─────────────────────┼─────────────────────┐
    │                     │                     │
┌───▼───┐           ┌────▼────┐           ┌───▼───┐
│warp-1 │           │ warp-2  │           │warp-N │
│:1081  │           │ :1082   │           │:10XX  │
└───────┘           └─────────┘           └───────┘
```

## Features

- **SOCKS5 Proxy** - With username/password authentication
- **Load Balancing** - Round-robin, least-connections, IP-hash
- **Auto Scaling** - Based on connections or response codes (429, 502, etc.)
- **Health Checking** - Automatic backend health monitoring
- **REST API** - Full management API
- **Prometheus Metrics** - /metrics/prometheus endpoint
- **Metrics History** - SQLite storage for historical data
- **Graceful Shutdown** - Proper cleanup on exit
- **Alerting** - Webhook notifications for events
- **Rolling Updates** - Zero-downtime container updates
- **CLI Tool** - `warpctl` for easy management

## Quick Start

### Build

```bash
make all  # Build both manager and CLI
```

### Run

```bash
# With config
make run

# Dev mode (no build)
make dev
```

### CLI Tool

```bash
# Show status
./warpctl status

# List containers
./warpctl containers

# Scale to 5
./warpctl scale 5

# Check health
./warpctl health

# Create container
./warpctl create
```

## Configuration

See `config.example.yaml` for full configuration.

```yaml
manager:
  api_port: 8080
  log_level: info

proxy:
  listen: ":1080"
  auth:
    enabled: true
    users:
      - user: "admin"
        pass: "password"
  timeout:
    connect: 5s
    idle: 30s

scaling:
  min: 1
  max: 10
  cooldown: 60s
  triggers:
    - name: rate-limit
      type: response_code
      response_code: 429
      threshold: 10
      window: 60s
      scale_direction: up
      scale_count: 1
      cooldown: 120s

loadbalancer:
  algorithm: roundrobin  # roundrobin | leastconn | iphash
  health_check:
    enabled: true
    interval: 10s
    timeout: 5s

docker:
  image: "ghcr.io/lowkruc/warp-proxy:latest"
  network: "warp-net"
```

## API Reference

### Proxy

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/proxy/stats | Proxy statistics |
| GET | /api/v1/proxy/connections | Active connections |

### Containers

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/containers | List containers |
| GET | /api/v1/containers/:id | Get container |
| POST | /api/v1/containers | Create container |
| DELETE | /api/v1/containers/:id | Delete container |
| POST | /api/v1/containers/:id/restart | Restart container |

### Scaling

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/scaling | Get scaling config |
| PUT | /api/v1/scaling | Update config |
| POST | /api/v1/scaling/scale/:count | Manual scale |
| GET | /api/v1/scaling/history | Scale events |

### Health & Metrics

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/health | Overall health |
| GET | /api/v1/health/containers | Container health |
| GET | /api/v1/metrics | Current metrics |
| GET | /api/v1/metrics/history | Historical metrics |
| GET | /metrics/prometheus | Prometheus format |

### Config

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/config | Get config |
| PUT | /api/v1/config | Update config |

## Examples

### Test Proxy

```bash
# Test with curl
curl --socks5-hostname admin:password@localhost:1080 https://cloudflare.com/cdn-cgi/trace

# Without auth
curl --socks5-hostname localhost:1080 https://cloudflare.com/cdn-cgi/trace
```

### Scale Manually

```bash
# Scale to 5 containers
curl -X POST http://localhost:8080/api/v1/scaling/scale/5 \
  -H "Authorization: Bearer token"

# Scale down to 1
curl -X POST http://localhost:8080/api/v1/scaling/scale/1
```

### Check Health

```bash
curl http://localhost:8080/api/v1/health
```

### Get Metrics

```bash
# Current metrics
curl http://localhost:8080/api/v1/metrics

# Historical (1 minute window)
curl http://localhost:8080/api/v1/metrics?window=1m

# Prometheus format
curl http://localhost:8080/metrics/prometheus
```

## How Scaling Works

### Response Code Scaling

```
1. Request → Proxy → Backend → Target
2. Target returns 429 (rate limit)
3. Proxy retries with next backend (new IP)
4. If success → Done
5. If all backends return 429 → Counter++
6. Counter > threshold → Scale UP
7. New container = New IP = Fresh rate limit
```

### Connection-based Scaling

```
1. Check active connections per container
2. If avg > threshold → Scale UP
3. If avg < threshold → Scale DOWN
4. Respect min/max limits
5. Respect cooldown period
```

## Project Structure

```
warp-proxy-manager/
├── cmd/manager/main.go      # Entry point
├── internal/
│   ├── api/                 # REST API
│   │   ├── handlers.go
│   │   └── middleware.go
│   ├── config/              # Configuration
│   │   └── config.go
│   ├── docker/              # Docker client
│   │   └── client.go
│   ├── proxy/               # SOCKS5 proxy
│   │   ├── proxy.go
│   │   ├── balancer.go
│   │   ├── tracker.go
│   │   └── health.go
│   ├── scaler/              # Auto-scaler
│   │   └── scaler.go
│   └── store/               # SQLite store
│       ├── store.go
│       └── metrics.go
├── api/openapi.yaml         # OpenAPI spec
├── config.example.yaml      # Example config
├── Makefile
└── README.md
```

## License

GPL-3.0
