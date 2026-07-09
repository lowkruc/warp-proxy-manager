package proxy

import (
	"hash/fnv"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Backend struct {
	ID            string
	Name          string
	Address       string  // host:port
	Port          int
	Healthy       bool
	Connections   int64
	LastHealth    time.Time
	ConsecutiveOK int
}

type LoadBalancer struct {
	mu         sync.RWMutex
	backends   []*Backend
	algorithm  string
	index      uint64
	tracker    *ConnectionTracker
}

func NewLoadBalancer(algorithm string) *LoadBalancer {
	return &LoadBalancer{
		backends:  make([]*Backend, 0),
		algorithm: algorithm,
		tracker:   NewConnectionTracker(),
	}
}

func (lb *LoadBalancer) AddBackend(id, name, address string, port int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.backends = append(lb.backends, &Backend{
		ID:      id,
		Name:    name,
		Address: address,
		Port:    port,
		Healthy: true,
	})
}

func (lb *LoadBalancer) RemoveBackend(id string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for i, b := range lb.backends {
		if b.ID == id {
			lb.backends = append(lb.backends[:i], lb.backends[i+1:]...)
			return
		}
	}
}

func (lb *LoadBalancer) UpdateHealth(id string, healthy bool) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, b := range lb.backends {
		if b.ID == id {
			b.Healthy = healthy
			b.LastHealth = time.Now()
			if healthy {
				b.ConsecutiveOK++
			} else {
				b.ConsecutiveOK = 0
			}
			return
		}
	}
}

func (lb *LoadBalancer) Next() *Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	healthy := lb.getHealthy()
	if len(healthy) == 0 {
		return nil
	}

	switch lb.algorithm {
	case "leastconn":
		return lb.leastConnections(healthy)
	case "iphash":
		return nil // handled separately with IP
	default: // roundrobin
		return lb.roundRobin(healthy)
	}
}

func (lb *LoadBalancer) NextForIP(srcIP string) *Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	healthy := lb.getHealthy()
	if len(healthy) == 0 {
		return nil
	}

	if lb.algorithm == "iphash" {
		return lb.ipHash(healthy, srcIP)
	}

	return lb.Next()
}

func (lb *LoadBalancer) NextSkipTried(tried map[string]bool) *Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	for _, b := range lb.backends {
		if b.Healthy && !tried[b.ID] {
			return b
		}
	}
	return nil
}

func (lb *LoadBalancer) GetBackends() []*Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	result := make([]*Backend, len(lb.backends))
	copy(result, lb.backends)
	return result
}

func (lb *LoadBalancer) GetHealthy() []*Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	return lb.getHealthy()
}

func (lb *LoadBalancer) getHealthy() []*Backend {
	var result []*Backend
	for _, b := range lb.backends {
		if b.Healthy {
			result = append(result, b)
		}
	}
	return result
}

func (lb *LoadBalancer) roundRobin(backends []*Backend) *Backend {
	if len(backends) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&lb.index, 1)
	return backends[idx%uint64(len(backends))]
}

func (lb *LoadBalancer) leastConnections(backends []*Backend) *Backend {
	if len(backends) == 0 {
		return nil
	}

	var minConn int64 = -1
	var result *Backend

	for _, b := range backends {
		conn := atomic.LoadInt64(&b.Connections)
		if minConn == -1 || conn < minConn {
			minConn = conn
			result = b
		}
	}

	return result
}

func (lb *LoadBalancer) ipHash(backends []*Backend, srcIP string) *Backend {
	if len(backends) == 0 {
		return nil
	}

	h := fnv.New32a()
	h.Write([]byte(srcIP))
	idx := h.Sum32()

	return backends[uint32(idx)%uint32(len(backends))]
}

func (lb *LoadBalancer) IncrementConnections(id string) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	for _, b := range lb.backends {
		if b.ID == id {
			atomic.AddInt64(&b.Connections, 1)
			return
		}
	}
}

func (lb *LoadBalancer) DecrementConnections(id string) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	for _, b := range lb.backends {
		if b.ID == id {
			atomic.AddInt64(&b.Connections, -1)
			return
		}
	}
}

func (lb *LoadBalancer) HealthyCount() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	return len(lb.getHealthy())
}

func (lb *LoadBalancer) AllHealthy429(responseCodes map[string]int) bool {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	healthy := lb.getHealthy()
	if len(healthy) == 0 {
		return false
	}

	for _, b := range healthy {
		if responseCodes[b.ID] < 429 || responseCodes[b.ID] >= 500 {
			return false
		}
	}

	return true
}

// CheckHealth performs health check on a backend
func (lb *LoadBalancer) CheckHealth(backend *Backend, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", backend.Address, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
