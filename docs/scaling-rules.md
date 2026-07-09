# Scaling Rules - Detailed Design

## Overview

Auto-scaling triggered by response codes (429, 502, etc.) with smart retry logic.

## Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                        Request Flow                              │
└─────────────────────────────────────────────────────────────────┘

Client Request
      │
      ▼
┌─────────────────┐
│  Select Backend │ ← Load Balancer (roundrobin)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ SOCKS5 Handshake│ ← with backend
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ SOCKS5 CONNECT  │ ← forward request
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Send Success    │ ← SOCKS5 reply 0x00 to client
│ to Client       │
└────────┬────────┘
         │
         ▼
┌─────────────────┐     ┌──────────────────┐
│ Peek Buffer     │────▶│ Check for 429    │
│ (512 bytes)     │     │ (500ms timeout)  │
└────────┬────────┘     └──────┬───────────┘
         │                     │
         │          ┌──────────┼──────────┐
         │          │          │          │
         │          ▼          ▼          ▼
         │    ┌──────────┐ ┌───────┐ ┌─────────┐
         │    │ Timeout  │ │ 429   │ │ Normal  │
         │    │ (no data)│ │Found  │ │ Data    │
         │    └────┬─────┘ └───┬───┘ └────┬────┘
         │         │           │          │
         │         │           │          ▼
         │         │           │    ┌───────────┐
         │         │           │    │ Forward   │
         │         │           │    │ bytes to  │
         │         │           │    │ client    │
         │         │           │    └─────┬─────┘
         │         │           │          │
         │         ▼           ▼          │
         │    ┌────────────────────┐      │
         │    │    proxy()        │◀─────┘
         │    │ (bidirectional)   │
         │    └────────┬─────────┘
         │             │
         │             │
         │    ┌────────┴────────┐
         │    │ 429 detected?   │
         │    └────────┬────────┘
         │             │
         │    ┌────────┴────────┐
         │    │                 │
         │    ▼                 ▼
         │ ┌──────┐       ┌──────────┐
         │ │  YES │       │   NO     │
         │ └──┬───┘       │ 2xx/3xx  │
         │    │           └────┬─────┘
         │    │                │
         │    ▼                ▼
         │ ┌────────────────────┐
         │ │  Track in counter  │
         │ │  (429 events)      │
         │ └─────────┬──────────┘
         │           │
         │           ▼
         │ ┌────────────────────┐
         │ │ Retry next backend │
         │ │ (max 3 retries)   │
         │ └─────────┬──────────┘
         │           │
         │           ▼
         │ ┌────────────────────┐
         │ │ All retries failed?│
         │ └─────────┬──────────┘
         │           │
         │    ┌──────┴──────┐
         │    │             │
         │    ▼             ▼
         │ ┌──────┐  ┌───────────┐
         │ │  YES │  │    NO     │
         │ └──┬───┘  │  200 OK   │
         │    │      └───────────┘
         │    ▼
         │ ┌────────────────────┐
         │ │ All backends 429?  │
         │ └─────────┬──────────┘
         │           │
         │    ┌──────┴──────┐
         │    │             │
         │    ▼             ▼
         │ ┌──────┐  ┌──────────┐
         │ │  YES │  │   NO     │
         │ └──┬───┘  │  wait    │
         │    │      └──────────┘
         │    ▼
         │ ┌────────────────────┐
         │ │ Counter > thresh?  │
         │ └─────────┬──────────┘
         │           │
         │    ┌──────┴──────┐
         │    │             │
         │    ▼             ▼
         │ ┌──────┐  ┌──────────┐
         │ │  NO  │  │ SCALE UP │
         │ └──────┘  └──────────┘
```

## Peek Buffer Mechanism

SOCKS5 proxies cannot directly read HTTP status codes because they operate at Layer 5 (session), not Layer 7 (application). After a successful SOCKS5 CONNECT, the proxy must peek at the initial response bytes to detect rate limiting.

### How It Works

```go
// After SOCKS5 CONNECT succeeds
peekBuf := make([]byte, 512)
backendConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
n, peekErr := backendConn.Read(peekBuf)
backendConn.SetReadDeadline(time.Time{}) // reset

if peekErr == nil && n > 0 {
    if contains429(peekBuf[:n]) {
        return 429, nil  // Rate limited
    }
    clientConn.Write(peekBuf[:n]) // Forward normal response
}
// Proceed with bidirectional proxy()
```

### Timeout Handling

| Scenario | Backend Response | Action | Client Delay |
|----------|-----------------|--------|--------------|
| Fast response | Data < 500ms | Check 429, forward if OK | ~0.5ms |
| Slow response | Timeout 500ms | Skip check, proxy() | 500ms |
| 429 response | "429" in first 512 bytes | Retry next backend | ~1ms |
| No data (protocol) | Client speaks first | Skip check, proxy() | 500ms |

## Scaling States

```
┌─────────────────────────────────────────────────────────────┐
│                     Scaling State Machine                    │
└─────────────────────────────────────────────────────────────┘

                    ┌──────────────────┐
                    │     STABLE       │
                    │ (normal traffic) │
                    └────────┬─────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
     ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
     │ 429 detected │ │ High conn    │ │ High latency │
     └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
            │                │                │
            ▼                ▼                ▼
     ┌──────────────────────────────────────────────┐
     │              INCREMENT COUNTER               │
     └──────────────────────┬───────────────────────┘
                            │
                            ▼
                   ┌────────────────┐
                   │Counter > thresh│
                   └────────┬───────┘
                            │
               ┌────────────┴────────────┐
               │                         │
               ▼                         ▼
        ┌──────────┐              ┌──────────┐
        │   NO     │              │   YES    │
        │  Wait    │              │Cooldown? │
        └──────────┘              └────┬─────┘
                                       │
                              ┌────────┴────────┐
                              │                 │
                              ▼                 ▼
                      ┌──────────┐       ┌──────────┐
                      │   YES    │       │   NO     │
                      │   Wait   │       │ SCALE UP │
                      └──────────┘       └────┬─────┘
                                              │
                                              ▼
                                     ┌────────────────┐
                                     │  SCALING UP    │
                                     │  Add N containers│
                                     └────────┬───────┘
                                              │
                                              ▼
                                     ┌────────────────┐
                                     │  COOLDOWN      │
                                     │  Wait X seconds│
                                     └────────┬───────┘
                                              │
                                              ▼
                                     ┌────────────────┐
                                     │    STABLE      │
                                     └────────────────┘
```

## Retry Logic

### Request Handler (SOCKS5 with Peek)

```go
func (ps *ProxyServer) handleConn(clientConn net.Conn) {
    // ... SOCKS5 handshake, read request ...
    
    maxRetries := ps.config.MaxRetries
    tried := make(map[string]bool)
    var lastErr error

    for retry := 0; retry < maxRetries; retry++ {
        backend := ps.balancer.NextSkipTried(tried)
        if backend == nil {
            break
        }
        tried[backend.ID] = true

        respCode, err := ps.tryBackend(clientConn, req, backend)
        if err != nil {
            lastErr = err
            continue
        }

        // Check response code (from peek buffer)
        if respCode == 429 {
            atomic.AddInt64(&ps.total429, 1)
            ps.metrics.Track429()
            ps.scaler.TrackResponseCode(backend.ID, 429)
            continue  // Try next backend
        }

        if respCode >= 500 {
            ps.metrics.Track5xx()
            ps.scaler.TrackResponseCode(backend.ID, respCode)
            continue
        }

        // Success - proxy() already running
        return
    }

    // All retries failed
    ps.sendError(clientConn, socks5RepFailure)
}

func (ps *ProxyServer) tryBackend(clientConn net.Conn, req *socks5Request, backend *Backend) (int, error) {
    backendConn, err := net.DialTimeout("tcp", backend.Address, ps.config.ConnectTimeout)
    if err != nil {
        return 0, err
    }
    defer backendConn.Close()

    // SOCKS5 handshake + CONNECT
    if err := ps.forwardHandshake(clientConn, backendConn); err != nil {
        return 0, err
    }
    if err := ps.forwardRequest(clientConn, backendConn, req); err != nil {
        return 0, err
    }
    ps.sendSuccess(clientConn)

    // [429 Detection] Peek first bytes with timeout
    peekBuf := make([]byte, 512)
    backendConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
    n, peekErr := backendConn.Read(peekBuf)
    backendConn.SetReadDeadline(time.Time{})

    if peekErr == nil && n > 0 {
        if contains429(peekBuf[:n]) {
            return 429, nil
        }
        // Forward peeked bytes to client
        clientConn.Write(peekBuf[:n])
    }

    // Bidirectional copy
    ps.proxy(clientConn, backendConn)
    return 200, nil
}
```

### Backend Selection

```go
func (b *Balancer) NextSkipTried(tried map[string]bool) *Backend {
    for i := 0; i < len(b.backends); i++ {
        backend := b.backends[b.currentIndex]
        b.currentIndex = (b.currentIndex + 1) % len(b.backends)
        
        if !tried[backend.ID] && backend.Healthy {
            return backend
        }
    }
    return nil
}
```

## Scaling Triggers

### Trigger 1: Response Code (429)

```yaml
- name: rate-limit
  type: response_code
  response_code: 429
  
  # Threshold logic:
  # Scale up if >= threshold 429s in window
  # AND all backends are returning 429
  
  threshold: 10           # 10 total 429s
  window: 60s             # in 60 second window
  
  # Additional condition (optional):
  all_backends_429: true  # Only scale if ALL backends hitting 429
  
  scale_direction: up
  scale_count: 1          # Add 1 container
  cooldown: 120s          # Wait 2 min before next scale
```

**Why `all_backends_429`?**
- If only 1 backend has 429, rotation handles it
- If ALL backends have 429, need new IP (new container)

### Trigger 2: Response Code (502/503)

```yaml
- name: server-error
  type: response_code
  response_code: 502
  
  threshold: 5
  window: 30s
  scale_direction: up
  scale_count: 2          # Add 2 containers (more aggressive)
  cooldown: 180s
```

### Trigger 3: Connection Count

```yaml
- name: high-connections
  type: connection
  
  # Scale up if connections per container > threshold
  threshold: 100          # 100 connections per container
  window: 30s
  scale_direction: up
  scale_count: 1
  cooldown: 120s

- name: low-connections
  type: connection
  
  # Scale down if connections per container < threshold
  threshold: 20
  window: 60s
  scale_direction: down
  scale_count: 1
  cooldown: 300s          # Longer cooldown for scale down
```

## Counter Implementation

```go
type ScalingCounter struct {
    mu       sync.RWMutex
    windows  map[string]*Window  // trigger_name -> window
}

type Window struct {
    Events    []Event
    Threshold int
    Duration  time.Duration
}

type Event struct {
    Timestamp time.Time
    BackendID string
    Code      int
}

// Track a 429 response
func (c *ScalingCounter) Track429(backendID string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    window := c.windows["rate-limit"]
    window.Events = append(window.Events, Event{
        Timestamp: time.Now(),
        BackendID: backendID,
        Code:      429,
    })
    
    // Cleanup old events
    window.Cleanup()
}

// Check if threshold exceeded
func (c *ScalingCounter) ShouldScale(triggerName string) bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    window := c.windows[triggerName]
    return len(window.Events) >= window.Threshold
}

// Check if ALL backends returning 429
func (c *ScalingCounter) AllBackends429() bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    window := c.windows["rate-limit"]
    
    // Get unique backends with 429
    backends := make(map[string]bool)
    for _, event := range window.Events {
        backends[event.BackendID] = true
    }
    
    // Compare with total healthy backends
    totalBackends := c.balancer.HealthyCount()
    
    return len(backends) >= totalBackends
}
```

## Scale Decision Logic

```go
func (s *Scaler) evaluate() {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // Check cooldown
    if time.Since(s.lastScale) < s.config.Cooldown {
        return
    }
    
    currentCount := s.docker.RunningCount()
    
    // Check scale up triggers
    for _, trigger := range s.config.Triggers {
        if !trigger.Enabled {
            continue
        }
        
        if trigger.Type == "response_code" {
            if !s.counter.ShouldScale(trigger.Name) {
                continue
            }
            
            // Check all_backends condition
            if trigger.AllBackends429 && !s.counter.AllBackends429() {
                continue  // Not all backends affected, rotation handles it
            }
            
            // Can we scale up?
            if currentCount >= s.config.Max {
                log.Warn("Cannot scale up, max reached")
                continue
            }
            
            // Do the scaling
            targetCount := min(currentCount + trigger.ScaleCount, s.config.Max)
            s.scale(targetCount, trigger.Name)
            return
        }
    }
    
    // Check scale down triggers
    for _, trigger := range s.config.Triggers {
        if trigger.Type == "connection" && trigger.ScaleDirection == "down" {
            avgConns := s.metrics.AvgConnectionsPerContainer()
            if avgConns < trigger.Threshold && currentCount > s.config.Min {
                targetCount := max(currentCount - trigger.ScaleCount, s.config.Min)
                s.scale(targetCount, trigger.Name)
                return
            }
        }
    }
}

func (s *Scaler) scale(targetCount int, reason string) {
    currentCount := s.docker.RunningCount()
    
    event := ScaleEvent{
        ID:        uuid.New(),
        Timestamp: time.Now(),
        From:      currentCount,
        To:        targetCount,
        Reason:    reason,
    }
    
    if targetCount > currentCount {
        // Scale up
        for i := 0; i < targetCount-currentCount; i++ {
            go s.docker.CreateContainer()
        }
    } else {
        // Scale down (remove oldest/unhealthy first)
        for i := 0; i < currentCount-targetCount; i++ {
            go s.docker.RemoveOldest()
        }
    }
    
    s.store.SaveScaleEvent(event)
    s.lastScale = time.Now()
}
```

## Example Scenarios

### Scenario 1: Rate Limit Hit (with Peek Buffer)

```
1. Client A → Backend 1 → SOCKS5 CONNECT OK
2. Peek 512 bytes → "HTTP/1.1 429 Too Many Requests"
3. Return 429 to retry loop
4. Client A → Backend 2 → SOCKS5 CONNECT OK
5. Peek 512 bytes → "HTTP/1.1 200 OK" → Forward to client ✅
6. No scaling needed

7. Client B → Backend 1 → 429 (peek detected)
8. Client B → Backend 2 → 429 (peek detected)
9. Client B → Backend 3 → 429 (peek detected)
10. ALL backends returning 429
11. Counter: 6 429s in 60s window
12. Threshold: 10 → Not yet

13. More requests... counter reaches 10
14. ALL backends still 429
15. SCALE UP → Add 1 container
16. New container gets new IP
17. Requests now succeed on new container ✅
```

### Scenario 2: Backend Failure

```
1. Backend 1 unhealthy (502)
2. Backend 2 healthy (200)
3. Rotation works, no scaling

4. Backend 1, 2, 3 all returning 502
5. Counter reaches threshold
6. SCALE UP → Add 2 containers
```

### Scenario 3: Traffic Spike

```
1. Connections per container: 50
2. Threshold: 100
3. No action

4. Connections spike to 120 per container
5. SCALE UP → Add 1 container
6. New distribution: 60 per container
```

## Cooldown Logic

```go
func (s *Scaler) evaluate() {
    // Check cooldown
    if time.Since(s.lastScale) < s.config.Scaling.Cooldown {
        return
    }
    // ... rest of evaluation
}
```
