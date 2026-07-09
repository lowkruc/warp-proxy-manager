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
│  Select Backend │ ← Load Balancer
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Forward Request │
└────────┬────────┘
         │
         ▼
┌─────────────────┐     ┌──────────────┐
│ Target Response │────▶│ Check Status │
└────────┬────────┘     └──────┬───────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
              ▼                ▼                ▼
         ┌────────┐       ┌────────┐       ┌────────┐
         │ 2xx/3xx│       │  429   │       │ 5xx/other│
         └────┬───┘       └────┬───┘       └────┬────┘
              │                │                │
              ▼                │                ▼
         ┌─────────┐          │           ┌─────────┐
         │ Return  │          │           │ Retry   │
         │ Success │          │           │ (max 3) │
         └─────────┘          │           └────┬────┘
                              │                │
                              ▼                ▼
                    ┌───────────────────────────────┐
                    │      All retries failed?      │
                    └───────────────┬───────────────┘
                                    │
                       ┌────────────┴────────────┐
                       │                         │
                       ▼                         ▼
              ┌─────────────┐           ┌─────────────┐
              │ Rotate to   │           │ All backends│
              │ next backend│           │ returned 429│
              │ (try others)│           └──────┬──────┘
              └─────────────┘                  │
                                               ▼
                                      ┌─────────────────┐
                                      │ Track in counter│
                                      │ (429 events)    │
                                      └────────┬────────┘
                                               │
                                               ▼
                                      ┌─────────────────┐
                                      │ Counter > threshold?
                                      └────────┬────────┘
                                               │
                                  ┌────────────┴────────────┐
                                  │                         │
                                  ▼                         ▼
                            ┌──────────┐            ┌──────────┐
                            │   NO     │            │   YES    │
                            │ (wait)   │            │ SCALE UP │
                            └──────────┘            └──────────┘
```

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

### Request Handler

```go
func (p *Proxy) handleRequest(clientConn net.Conn, targetHost string) {
    maxRetries := 3
    backends := p.balancer.GetBackends()
    
    var lastErr error
    tried := make(map[string]bool)
    
    for retry := 0; retry < maxRetries && retry < len(backends); retry++ {
        // Select backend (skip already tried)
        backend := p.balancer.NextSkipTried(tried)
        if backend == nil {
            break
        }
        tried[backend.ID] = true
        
        // Forward request
        resp, err := p.forward(clientConn, backend, targetHost)
        if err != nil {
            lastErr = err
            continue
        }
        
        // Check response code
        switch {
        case resp.StatusCode == 429:
            // Rate limited - track and retry
            p.scaler.TrackResponseCode(backend.ID, 429)
            p.metrics.Incr429(backend.ID)
            continue  // Try next backend
            
        case resp.StatusCode >= 500:
            // Server error - retry
            p.scaler.TrackResponseCode(backend.ID, resp.StatusCode)
            continue
            
        default:
            // Success - return to client
            p.writeResponse(clientConn, resp)
            return
        }
    }
    
    // All retries failed
    p.scaler.TrackAllFailed(targetHost, lastErr)
    p.writeError(clientConn, 502, "All backends failed")
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

### Scenario 1: Rate Limit Hit

```
1. Client A → Backend 1 → Target → 429
2. Client A → Backend 2 → Target → 200 ✅ (rotation works)
3. No scaling needed

4. Client B → Backend 1 → 429
5. Client B → Backend 2 → 429
6. Client B → Backend 3 → 429
7. ALL backends returning 429
8. Counter: 6 429s in 60s window
9. Threshold: 10 → Not yet

10. More requests... counter reaches 10
11. ALL backends still 429
12. SCALE UP → Add 1 container
13. New container gets new IP
14. Requests now succeed on new container ✅
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
type CooldownManager struct {
    mu       sync.RWMutex
    lastTime map[string]time.Time  // trigger_name -> last_scale_time
}

func (c *CooldownManager) CanScale(triggerName string, cooldown time.Duration) bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    last, ok := c.lastTime[triggerName]
    if !ok {
        return true
    }
    
    return time.Since(last) >= cooldown
}

func (c *CooldownManager) RecordScale(triggerName string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.lastTime[triggerName] = time.Now()
}
```
