# Warp Proxy Manager - Planning

## Overview

Go-based load balancer & manager for warp-proxy containers.

```
┌─────────────────────────────────────────────────────────────┐
│                    warp-proxy-manager                        │
│                                                             │
│  ┌─────────────┐  ┌─────────────┐                           │
│  │   REST API  │  │  OpenAPI    │                           │
│  └──────┬──────┘  └──────┬──────┘                           │
│         │                │                                  │
│         └────────┬───────┘                                  │
│                  │                                          │
│            ┌─────▼─────┐                                   │
│            │   Core    │                                   │
│            └─────┬─────┘                                   │
│                  │                                          │
│    ┌─────────────┼─────────────┐                           │
│    │             │             │                            │
│ ┌──▼──┐      ┌──▼──┐      ┌──▼──┐                        │
│ │Proxy│      │Scale│      │Monit│                         │
│ │+Auth│      │     │      │     │                         │
│ └──┬──┘      └──┬──┘      └──┬──┘                         │
│    │            │             │                            │
│    └────────────┼─────────────┘                            │
│                 │                                           │
└─────────────────┼───────────────────────────────────────────┘
                  │
           ┌──────▼──────┐
           │   Docker    │
           └──────┬──────┘
                  │
    ┌─────────────┼─────────────┐
    │             │             │
┌───▼───┐   ┌───▼───┐   ┌───▼───┐
│warp-1 │   │warp-2 │   │warp-N │
│:1081  │   │:1082  │   │:10XX  │
└───────┘   └───────┘   └───────┘
```

## Decisions (Confirmed)

| Item | Decision |
|------|----------|
| Manager API Port | `:8080` |
| SOCKS5 Proxy Port | `:1080` (exposed) |
| Authentication | Required (user/pass) |
| Dashboard | No (API only + OpenAPI spec) |
| Scaling Trigger | Connections + Custom response codes |

## Auto-Scaling Rules

### Rule 1: Connection-based

```
IF active_connections > threshold THEN scale_up
IF active_connections < threshold THEN scale_down
```

### Rule 2: Response code-based (Custom)

```yaml
scaling:
  rules:
    - name: rate-limit
      response_code: 429
      threshold: 10          # 10x 429 in window
      window: 60s            # time window
      action: scale_up
      scale_count: 1         # add 1 container
      cooldown: 120s         # wait before next scale
    
    - name: server-error
      response_code: 502
      threshold: 5
      window: 30s
      action: scale_up
      scale_count: 2         # add 2 containers
      cooldown: 180s
```

### Rule 3: Latency-based

```
IF avg_latency > threshold THEN scale_up
```

## Config Structure

```yaml
# Manager
manager:
  api_port: 8080
  log_level: info

# Proxy
proxy:
  listen: ":1080"
  auth:
    enabled: true
    users:
      - user: "admin"
        pass: "$2a$10$..."  # bcrypt hash
  timeout:
    connect: 5s
    idle: 30s

# Scaling
scaling:
  min: 1
  max: 10
  strategy: connection  # connection | response_code | combined
  cooldown: 60s
  
  triggers:
    connection:
      scale_up_threshold: 100    # connections per container
      scale_down_threshold: 20
    
    response_code:
      - code: 429
        threshold: 10
        window: 60s
        action: up
        count: 1
      - code: 502
        threshold: 5
        window: 30s
        action: up
        count: 2

# Load Balancer
loadbalancer:
  algorithm: roundrobin  # roundrobin | leastconn | iphash
  health_check:
    enabled: true
    interval: 10s
    timeout: 5s
    unhealthy_threshold: 3

# Docker
docker:
  image: "ghcr.io/lowkruc/warp-proxy:latest"
  network: "warp-net"
  prefix: "warp"
  volumes:
    warp: "/var/lib/cloudflare-warp"
  env:
    WARP_SLEEP: "3"
    WARP_WAIT_IFACE: "1"
  resources:
    memory: "100m"
    cpus: "0.5"
```

## API Endpoints

### Proxy

```
GET  /api/v1/proxy/stats          - Proxy statistics
GET  /api/v1/proxy/connections    - Active connections
```

### Containers

```
GET    /api/v1/containers          - List containers
GET    /api/v1/containers/:id      - Container details
POST   /api/v1/containers          - Create container
DELETE /api/v1/containers/:id      - Remove container
POST   /api/v1/containers/:id/restart - Restart
POST   /api/v1/containers/:id/scale   - Scale specific container (future)
```

### Scaling

```
GET    /api/v1/scaling             - Get scaling config
PUT    /api/v1/scaling             - Update scaling config
POST   /api/v1/scaling/scale/:n    - Manual scale to N
GET    /api/v1/scaling/history     - Scale events history
```

### Health

```
GET    /api/v1/health              - Overall health
GET    /api/v1/health/containers   - Container health status
```

### Metrics

```
GET    /api/v1/metrics             - Current metrics (JSON)
GET    /metrics                    - Prometheus format
```

### Config

```
GET    /api/v1/config              - Get config
PUT    /api/v1/config              - Update config (partial)
```

## Auth Implementation

### Proxy Auth (SOCKS5/HTTP)

SOCKS5 username/password auth:
```
Client → SOCKS5 Auth → Manager verifies → Proxy to backend
```

### API Auth

Bearer token or Basic auth:
```
Authorization: Bearer <token>
# or
Authorization: Basic base64(user:pass)
```

## Data Structures

### Container

```go
type Container struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Status      ContainerStatus   `json:"status"`
    Port        int               `json:"port"`
    StartedAt   time.Time         `json:"started_at"`
    LastHealth  time.Time         `json:"last_health"`
    Connections  int64            `json:"connections"`
    Latency     float64           `json:"latency_ms"`
    Labels      map[string]string `json:"labels"`
}

type ContainerStatus string

const (
    StatusRunning   ContainerStatus = "running"
    StatusStopped   ContainerStatus = "stopped"
    StatusStarting  ContainerStatus = "starting"
    StatusUnhealthy ContainerStatus = "unhealthy"
)
```

### ScaleEvent

```go
type ScaleEvent struct {
    ID        string    `json:"id"`
    Timestamp time.Time `json:"timestamp"`
    From      int       `json:"from"`
    To        int       `json:"to"`
    Reason    string    `json:"reason"`
    Trigger   string    `json:"trigger"`
}
```

### Metrics

```go
type Metrics struct {
    Timestamp         time.Time `json:"timestamp"`
    ActiveConnections int64     `json:"active_connections"`
    TotalRequests     int64     `json:"total_requests"`
    RequestsPerSecond float64   `json:"rps"`
    AvgLatency        float64   `json:"avg_latency_ms"`
    ErrorRate         float64   `json:"error_rate"`
    ContainersRunning int       `json:"containers_running"`
    ContainersHealthy int       `json:"containers_healthy"`
    
    // Response code counts
    ResponseCodes     map[int]int64 `json:"response_codes"`
}
```

## Project Structure

```
warp-proxy-manager/
├── cmd/
│   └── manager/
│       └── main.go
├── internal/
│   ├── api/
│   │   ├── router.go
│   │   ├── handlers.go
│   │   └── middleware.go
│   ├── auth/
│   │   ├── auth.go          # Auth logic
│   │   └── socks5.go        # SOCKS5 auth
│   ├── proxy/
│   │   ├── proxy.go         # SOCKS5/HTTP proxy
│   │   ├── balancer.go      # Load balancing
│   │   └── tracker.go       # Connection tracking
│   ├── scaler/
│   │   ├── scaler.go        # Auto-scaling engine
│   │   ├── rules.go         # Scaling rules
│   │   └── metrics.go       # Metrics collection
│   ├── docker/
│   │   ├── client.go        # Docker SDK wrapper
│   │   ├── container.go     # Container lifecycle
│   │   └── health.go        # Health checking
│   ├── store/
│   │   ├── store.go         # SQLite store
│   │   ├── containers.go    # Container state
│   │   └── metrics.go       # Metrics history
│   └── config/
│       ├── config.go        # Config loading
│       └── validate.go      # Config validation
├── api/
│   └── openapi.yaml         # OpenAPI spec
├── web/
│   └── static/              # Future dashboard
├── config.example.yaml
├── go.mod
└── go.sum
```

## Implementation Phases

### Phase 1: Core Proxy (MVP)

- [ ] SOCKS5 proxy with auth
- [ ] Round-robin load balancing
- [ ] Manual container management (start/stop)
- [ ] Basic health checking
- [ ] Config file loading

### Phase 2: Scaling

- [ ] Connection-based auto-scaling
- [ ] Response code-based scaling (429, 502, etc.)
- [ ] Cooldown management
- [ ] Scale events history

### Phase 3: API & Store

- [ ] REST API endpoints
- [ ] OpenAPI spec
- [ ] SQLite for state/metrics
- [ ] Prometheus metrics

### Phase 4: Polish

- [ ] Graceful shutdown
- [ ] Rolling updates
- [ ] Alerting (webhook)
- [ ] CLI tool

## Open Questions

1. **SOCKS5 auth:** Use standard SOCKS5 username/password or custom?
2. **Response code detection:** How to detect response codes in SOCKS5? (SOCKS5 doesn't expose HTTP status codes directly)
   - Option A: Only works in HTTP proxy mode
   - Option B: Wrap requests to detect upstream codes
   - Option C: Use health endpoint that returns specific codes
3. **State persistence:** SQLite for metrics history retention?

## Next Steps

1. Confirm remaining questions
2. Create OpenAPI spec
3. Set up Go project
4. Implement Phase 1
