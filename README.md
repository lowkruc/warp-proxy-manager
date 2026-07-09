# Warp Proxy Manager

Dynamic load balancer and manager for [warp-docker](https://github.com/lowkruc/warp-docker) containers. Auto-scaling, SOCKS5 proxy, REST API, and Docker deployment.

## Features

- **SOCKS5 Proxy** - Forward proxy with load balancing
- **Auto-Scaling** - Scale containers based on connection count or response codes (429, 5xx)
- **Health Checking** - Automatic backend health monitoring
- **Load Balancing** - Round-robin, least connections, or IP hash
- **REST API** - Full API with OpenAPI spec
- **Prometheus Metrics** - Export metrics for monitoring
- **Rolling Updates** - Update backends without downtime
- **Docker Management** - Auto-create and manage warp-proxy containers

## Quick Start

```bash
# Clone and run
git clone https://github.com/lowkruc/warp-proxy-manager.git
cd warp-proxy-manager
docker compose up -d
```

## Configuration

Copy and edit config file:

```bash
cp config.example.yaml config.yaml
```

### Config Reference

```yaml
manager:
  api_port: 8080                    # REST API port
  log_level: info                   # debug, info, warn, error

proxy:
  listen: ":1080"                   # SOCKS5 proxy listen address
  auth:
    enabled: false                  # Enable SOCKS5 authentication
    users:
      - username: admin
        password: admin123
  timeout:
    connect: 5s                     # Backend connection timeout
    idle: 30s                       # Idle connection timeout

scaling:
  min: 3                            # Minimum containers
  max: 10                           # Maximum containers
  cooldown: 60s                     # Cooldown between scaling actions
  triggers: []                      # Custom scaling triggers

loadbalancer:
  algorithm: roundrobin             # roundrobin, leastconn, iphash
  health_check:
    enabled: false

docker:
  image: "ghcr.io/lowkruc/warp-proxy:latest"
  network: "warp-net"
  prefix: "warp-proxy"
  memory_limit: "150m"              # Container memory limit
  cpu_limit: "0.1"                  # Container CPU limit (0.1 = 10%)
  env:
    WARP_SLEEP: "5"                 # Startup delay
    WARP_ROTATION_INTERVAL: "60"    # IP rotation interval (minutes)
```

## Usage

### Connect via SOCKS5

```bash
# Without auth
curl --socks5-hostname localhost:1080 https://ifconfig.me

# With auth
curl --socks5-hostname admin:admin123@localhost:1080 https://ifconfig.me
```

### REST API

```bash
# List containers
curl http://localhost:8080/api/v1/containers

# Get proxy stats
curl http://localhost:8080/api/v1/proxy/stats

# Create container
curl -X POST http://localhost:8080/api/v1/containers

# Remove container
curl -X DELETE http://localhost:8080/api/v1/containers/{id}
```

### CLI Tool (warpctl)

```bash
# List containers
./warpctl containers

# Create container
./warpctl create

# Remove container
./warpctl remove {id}

# Get stats
./warpctl stats
```

## API Documentation

Full OpenAPI spec: [api/openapi.yaml](api/openapi.yaml)

## How It Works

1. **Manager** creates warp-proxy containers with separate volumes (no shared registration)
2. **Health Checker** monitors container connectivity
3. **Load Balancer** distributes traffic across healthy backends
4. **Scaler** auto-creates/removes containers based on load
5. **WARP Rotation** changes IPs periodically for rate limit bypass

## Architecture

```
Client → Manager (SOCKS5) → warp-proxy-1 (WARP)
                           → warp-proxy-2 (WARP)
                           → warp-proxy-3 (WARP)
```

Each warp-proxy container:
- Runs独立 WARP tunnel with unique registration
- Rotates IP based on `WARP_ROTATION_INTERVAL`
- Bounded by memory (150MB) and CPU (0.1) limits
- Auto-restarts on failure

## Development

```bash
# Build
go build -o warp-proxy-manager ./cmd/manager/
go build -o warpctl ./cmd/cli/

# Run locally
./warp-proxy-manager -config config.yaml

# Docker
docker compose up -d
```

## License

MIT
