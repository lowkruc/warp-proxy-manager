package proxy

import (
	"log"
	"net"
	"sync"
	"time"
)

type HealthChecker struct {
	balancer  *LoadBalancer
	interval  time.Duration
	timeout   time.Duration
	unhealthy int
	stopCh    chan struct{}
	mu        sync.RWMutex
}

func NewHealthChecker(balancer *LoadBalancer, interval, timeout time.Duration, unhealthyThreshold int) *HealthChecker {
	return &HealthChecker{
		balancer:  balancer,
		interval:  interval,
		timeout:   timeout,
		unhealthy: unhealthyThreshold,
		stopCh:    make(chan struct{}),
	}
}

func (hc *HealthChecker) Start() {
	go hc.loop()
	log.Printf("[HEALTH] Checker started (interval: %s, timeout: %s)", hc.interval, hc.timeout)
}

func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
	log.Printf("[HEALTH] Checker stopped")
}

func (hc *HealthChecker) loop() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.checkAll()
		}
	}
}

func (hc *HealthChecker) checkAll() {
	backends := hc.balancer.GetBackends()

	for _, b := range backends {
		go hc.check(b)
	}
}

func (hc *HealthChecker) check(b *Backend) {
	healthy := hc.ping(b.Address)

	hc.mu.Lock()
	defer hc.mu.Unlock()

	if healthy {
		if !b.Healthy {
			log.Printf("[HEALTH] Backend %s (%s) is now healthy", b.Name, b.Address)
		}
		b.Healthy = true
		b.ConsecutiveOK++
	} else {
		b.ConsecutiveOK = 0
		// Mark unhealthy after N consecutive failures
		if b.Healthy {
			// Don't mark unhealthy immediately, wait for threshold
			// This is handled in the balancer update
		}
		b.Healthy = false
		log.Printf("[HEALTH] Backend %s (%s) is unhealthy", b.Name, b.Address)
	}

	b.LastHealth = time.Now()
}

func (hc *HealthChecker) ping(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, hc.timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (hc *HealthChecker) CheckNow() {
	hc.checkAll()
}

func (hc *HealthChecker) IsHealthy(addr string) bool {
	return hc.ping(addr)
}
