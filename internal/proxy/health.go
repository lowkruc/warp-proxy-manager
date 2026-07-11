package proxy

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// DockerClient abstracts docker operations needed by health checker
type DockerClient interface {
	RestartContainer(ctx context.Context, id string) error
}

type HealthChecker struct {
	balancer           *LoadBalancer
	dockerClient       DockerClient
	interval           time.Duration
	timeout            time.Duration
	unhealthyThreshold int
	healthyThreshold   int
	stopCh             chan struct{}
	mu                 sync.RWMutex
}

func NewHealthChecker(balancer *LoadBalancer, dockerClient DockerClient, interval, timeout time.Duration, unhealthyThreshold int) *HealthChecker {
	return &HealthChecker{
		balancer:           balancer,
		dockerClient:       dockerClient,
		interval:           interval,
		timeout:            timeout,
		unhealthyThreshold: unhealthyThreshold,
		healthyThreshold:   unhealthyThreshold, // same as unhealthy by default
		stopCh:             make(chan struct{}),
	}
}

func (hc *HealthChecker) Start() {
	go hc.loop()
	log.Printf("[HEALTH] Started (interval: %s, timeout: %s, unhealthy_threshold: %d)",
		hc.interval, hc.timeout, hc.unhealthyThreshold)
}

func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
	log.Printf("[HEALTH] Stopped")
}

func (hc *HealthChecker) loop() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	// Run initial check immediately
	hc.checkAll()

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
	healthy := hc.socks5Check(b.Address)

	hc.mu.Lock()
	defer hc.mu.Unlock()

	if healthy {
		b.ConsecutiveFail = 0
		b.ConsecutiveOK++

		if !b.Healthy {
			// Only mark healthy after N consecutive successes
			if b.ConsecutiveOK >= hc.healthyThreshold {
				log.Printf("[HEALTH] Backend %s (%s) is now healthy (ok=%d)", b.Name, b.Address, b.ConsecutiveOK)
				b.Healthy = true
			}
		}
	} else {
		b.ConsecutiveOK = 0
		b.ConsecutiveFail++

		if b.Healthy && b.ConsecutiveFail >= hc.unhealthyThreshold {
			b.Healthy = false
			log.Printf("[HEALTH] Backend %s (%s) marked unhealthy after %d failures, restarting...",
				b.Name, b.Address, b.ConsecutiveFail)
			hc.restartContainer(b)
		} else if !b.Healthy {
			log.Printf("[HEALTH] Backend %s (%s) still unhealthy (fail=%d/%d)",
				b.Name, b.Address, b.ConsecutiveFail, hc.unhealthyThreshold)
		}
	}

	b.LastHealth = time.Now()
}

func (hc *HealthChecker) restartContainer(b *Backend) {
	if hc.dockerClient == nil {
		log.Printf("[HEALTH] No docker client, cannot restart %s", b.Name)
		return
	}

	log.Printf("[HEALTH] Restarting container %s (%s)...", b.Name, b.ID)
	if err := hc.dockerClient.RestartContainer(context.Background(), b.ID); err != nil {
		log.Printf("[HEALTH] Failed to restart %s: %v", b.Name, err)
	} else {
		log.Printf("[HEALTH] Restarted %s, resetting health counters", b.Name)
		// Reset counters after restart — container needs time to re-init
		b.ConsecutiveFail = 0
		b.ConsecutiveOK = 0
		b.Healthy = false
	}
}

// socks5Check does a full SOCKS5 handshake + CONNECT to verify the proxy actually works
func (hc *HealthChecker) socks5Check(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, hc.timeout)
	if err != nil {
		return false
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(hc.timeout))

	// SOCKS5 greeting: version 5, 1 method, no-auth
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return false
	}

	// Read greeting response
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return false
	}
	if greeting[0] != 0x05 || greeting[1] != 0x00 {
		return false
	}

	// SOCKS5 CONNECT request to test target
	// Use domain to also test DNS resolution through WARP
	target := "cp.cloudflare.com"
	port := uint16(443)

	req := make([]byte, 0, 7+len(target))
	req = append(req, 0x05, 0x01, 0x00) // VER CMD RSV
	req = append(req, 0x03)              // ATYP domain
	req = append(req, byte(len(target)))
	req = append(req, []byte(target)...)

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	req = append(req, portBytes...)

	if _, err := conn.Write(req); err != nil {
		return false
	}

	// Read CONNECT reply (at least 10 bytes)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return false
	}

	// reply[1] == 0x00 means success
	return reply[1] == 0x00
}

func (hc *HealthChecker) CheckNow() {
	hc.checkAll()
}

func (hc *HealthChecker) IsHealthy(addr string) bool {
	return hc.socks5Check(addr)
}
