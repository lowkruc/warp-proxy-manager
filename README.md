# Warp Proxy Manager

Dynamic load balancer & manager for warp-proxy containers.

## Features

- **Dynamic Scaling** - Auto-scale based on connections or response codes
- **Load Balancing** - Round-robin, least-connections, IP-hash
- **SOCKS5 Proxy** - With username/password authentication
- **Response Code Monitoring** - Scale on429, 502, etc.
- **REST API** - Full management API with OpenAPI spec

## Quick Start

```bash
# Build
go build -o warp-proxy-manager ./cmd/manager/

# Run
./warp-proxy-manager -config config.yaml
```

## Architecture

```
Client → SOCKS5 Proxy → Load Balancer → Warp Containers
              │                              │
              └── Auth ──────────────────────┘
              │
              └── Response Code Monitor → Scaler → Docker API
```

## Configuration

See `config.example.yaml` for full configuration.

### Key Settings

```yaml
scaling:
  min: 1
  max: 10
  triggers:
    - name: rate-limit
      type: response_code
      response_code: 429
      threshold: 10
      scale_direction: up
      scale_count: 1
```

## API

- **Port:** 8080
- **Auth:** Basic auth or Bearer token

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/proxy/stats | Proxy statistics |
| GET | /api/v1/containers | List containers |
| POST | /api/v1/containers | Create container |
| DELETE | /api/v1/containers/:id | Remove container |
| POST | /api/v1/scaling/scale/:n | Manual scale |
| GET | /api/v1/health | Health check |

See `api/openapi.yaml` for full API spec.

## Examples

### Scale to 5 containers

```bash
curl -X POST http://localhost:8080/api/v1/scaling/scale/5 \
  -H "Authorization: Bearer token"
```

### Check health

```bash
curl http://localhost:8080/api/v1/health
```

## How It Works

1. SOCKS5 proxy receives requests
2. Load balancer selects backend container
3. Request forwarded to warp-proxy container
4. If 429 response, retries with next backend
5. If all backends return 429, triggers scale up
6. Scaler creates new container with new IP
7. New IP has fresh rate limit

## License

GPL-3.0
