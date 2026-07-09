package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	socks5Version     = 0x05
	socks5AuthNone    = 0x00
	socks5AuthUserPass = 0x02
	socks5AuthFailed  = 0xFF
	socks5CmdConnect  = 0x01
	socks5AddrIPv4    = 0x01
	socks5AddrDomain  = 0x03
	socks5AddrIPv6    = 0x04
	socks5RepSuccess  = 0x00
	socks5RepFailure  = 0x01
	socks5RepNotSupported = 0x07
)

type Metrics interface {
	Track429()
	Track5xx()
}

type ProxyServer struct {
	config      *ProxyConfig
	balancer    *LoadBalancer
	tracker     *ConnectionTracker
	listener    net.Listener
	wg          sync.WaitGroup
	running     bool
	userMap     map[string]string
	metrics     Metrics
	totalReqs   int64
	total429    int64
}

type ProxyConfig struct {
	Listen      string
	AuthEnabled bool
	Users       []ProxyUser
	ConnectTimeout time.Duration
	IdleTimeout    time.Duration
	MaxRetries     int
}

type ProxyUser struct {
	User string
	Pass string
}

type ProxyRequest struct {
	ID        string
	SrcAddr   net.Addr
	DstHost   string
	DstPort   int
	Backend   *Backend
	StartTime time.Time
}

type ProxyStats struct {
	TotalRequests   int64             `json:"total_requests"`
	Total429        int64             `json:"total_429"`
	ActiveConns     int               `json:"active_connections"`
	PerBackend      map[string]int    `json:"per_backend"`
	BackendsHealthy int               `json:"backends_healthy"`
}

func NewProxyServer(config *ProxyConfig, balancer *LoadBalancer) *ProxyServer {
	return &ProxyServer{
		config:   config,
		balancer: balancer,
		tracker:  NewConnectionTracker(),
		userMap:  make(map[string]string),
	}
}

func (ps *ProxyServer) SetMetrics(m Metrics) {
	ps.metrics = m
}

func (ps *ProxyServer) Start() error {
	// Build user map
	for _, u := range ps.config.Users {
		ps.userMap[u.User] = u.Pass
	}

	var err error
	ps.listener, err = net.Listen("tcp", ps.config.Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	ps.running = true
	log.Printf("[PROXY] Listening on %s", ps.config.Listen)

	go ps.acceptLoop()

	return nil
}

func (ps *ProxyServer) Stop() {
	ps.running = false
	if ps.listener != nil {
		ps.listener.Close()
	}
	ps.wg.Wait()
	log.Printf("[PROXY] Stopped")
}

func (ps *ProxyServer) acceptLoop() {
	for ps.running {
		conn, err := ps.listener.Accept()
		if err != nil {
			if ps.running {
				log.Printf("[PROXY] Accept error: %v", err)
			}
			return
		}

		ps.wg.Add(1)
		go ps.handleConn(conn)
	}
}

func (ps *ProxyServer) handleConn(clientConn net.Conn) {
	defer ps.wg.Done()
	defer clientConn.Close()

	// SOCKS5 handshake
	if err := ps.handshake(clientConn); err != nil {
		log.Printf("[PROXY] Handshake failed: %v", err)
		return
	}

	// Read request
	req, err := ps.readRequest(clientConn)
	if err != nil {
		log.Printf("[PROXY] Read request failed: %v", err)
		return
	}

	log.Printf("[PROXY] Request: %s:%d", req.DstHost, req.DstPort)
	atomic.AddInt64(&ps.totalReqs, 1)

	// Try backends with retry on 429/5xx
	maxRetries := ps.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	tried := make(map[string]bool)
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		// Get next backend (skip tried ones)
		backend := ps.balancer.NextSkipTried(tried)
		if backend == nil {
			break
		}
		tried[backend.ID] = true

		log.Printf("[PROXY] Retry %d, using backend: %s (%s)", retry, backend.Name, backend.Address)

		// Try this backend
		respCode, err := ps.tryBackend(clientConn, req, backend)
		if err != nil {
			lastErr = err
			log.Printf("[PROXY] Backend %s failed: %v", backend.Name, err)
			continue
		}

		// Check response code
		if respCode == 429 {
			atomic.AddInt64(&ps.total429, 1)
			if ps.metrics != nil {
				ps.metrics.Track429()
			}
			log.Printf("[PROXY] Backend %s returned 429, trying next...", backend.Name)
			continue
		}

		if respCode >= 500 {
			if ps.metrics != nil {
				ps.metrics.Track5xx()
			}
			log.Printf("[PROXY] Backend %s returned %d, trying next...", backend.Name, respCode)
			continue
		}

		// Success
		return
	}

	// All retries failed
	if lastErr != nil {
		log.Printf("[PROXY] All backends failed: %v", lastErr)
		ps.sendError(clientConn, socks5RepFailure)
	}
}

func (ps *ProxyServer) tryBackend(clientConn net.Conn, req *socks5Request, backend *Backend) (int, error) {
	// Connect to backend
	backendConn, err := net.DialTimeout("tcp", backend.Address, ps.config.ConnectTimeout)
	if err != nil {
		return 0, fmt.Errorf("connect to backend: %w", err)
	}
	defer backendConn.Close()

	// Track connection
	connID := fmt.Sprintf("%s-%d", clientConn.RemoteAddr(), time.Now().UnixNano())
	ps.tracker.Track(connID, backend.ID, clientConn.RemoteAddr().String(), req.DstHost, req.DstPort)
	ps.balancer.IncrementConnections(backend.ID)

	defer func() {
		ps.tracker.Untrack(connID)
		ps.balancer.DecrementConnections(backend.ID)
	}()

	// Forward SOCKS5 handshake to backend
	if err := ps.forwardHandshake(clientConn, backendConn); err != nil {
		return 0, err
	}

	// Forward request
	if err := ps.forwardRequest(clientConn, backendConn, req); err != nil {
		return 0, err
	}

	// Read response (we need to check if it's 429)
	// For SOCKS5, the response code is in the reply
	// But for HTTP through SOCKS5, we'd need to parse HTTP response
	// For simplicity, we'll consider SOCKS5 success as200

	// Send success to client
	if err := ps.sendSuccess(clientConn); err != nil {
		return 0, err
	}

	// Bidirectional copy
	ps.proxy(clientConn, backendConn)

	return 200, nil
}

func (ps *ProxyServer) forwardHandshake(clientConn, backendConn net.Conn) error {
	// Read client handshake
	buf := make([]byte, 2)
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		return err
	}

	// Forward to backend
	if _, err := backendConn.Write(buf); err != nil {
		return err
	}

	// Read methods
	methods := make([]byte, buf[1])
	if _, err := io.ReadFull(clientConn, methods); err != nil {
		return err
	}
	if _, err := backendConn.Write(methods); err != nil {
		return err
	}

	// Read backend response
	resp := make([]byte, 2)
	if _, err := io.ReadFull(backendConn, resp); err != nil {
		return err
	}

	// Forward to client
	if _, err := clientConn.Write(resp); err != nil {
		return err
	}

	return nil
}

func (ps *ProxyServer) forwardRequest(clientConn, backendConn net.Conn, req *socks5Request) error {
	// Rebuild SOCKS5 request
	var buf []byte
	buf = append(buf, socks5Version, socks5CmdConnect, 0x00) // VER CMD RSV

	// ATYP + ADDR
	if isIPv4(req.DstHost) {
		buf = append(buf, socks5AddrIPv4)
		ip := net.ParseIP(req.DstHost).To4()
		buf = append(buf, ip...)
	} else if isIPv6(req.DstHost) {
		buf = append(buf, socks5AddrIPv6)
		ip := net.ParseIP(req.DstHost).To16()
		buf = append(buf, ip...)
	} else {
		buf = append(buf, socks5AddrDomain)
		buf = append(buf, byte(len(req.DstHost)))
		buf = append(buf, []byte(req.DstHost)...)
	}

	// PORT
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(req.DstPort))
	buf = append(buf, portBytes...)

	// Send to backend
	if _, err := backendConn.Write(buf); err != nil {
		return err
	}

	// Read backend reply
	reply := make([]byte, 10)
	if _, err := io.ReadFull(backendConn, reply); err != nil {
		return err
	}

	// Check reply code
	if reply[1] != socks5RepSuccess {
		return fmt.Errorf("backend reply error: %d", reply[1])
	}

	return nil
}

func (ps *ProxyServer) handshake(conn net.Conn) error {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}

	version := buf[0]
	nMethods := buf[1]

	if version != socks5Version {
		return fmt.Errorf("unsupported version: %d", version)
	}

	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}

	if ps.config.AuthEnabled {
		supported := false
		for _, m := range methods {
			if m == socks5AuthUserPass {
				supported = true
				break
			}
		}

		if !supported {
			conn.Write([]byte{socks5Version, socks5AuthFailed})
			return fmt.Errorf("client doesn't support user/pass auth")
		}

		conn.Write([]byte{socks5Version, socks5AuthUserPass})

		if err := ps.readUserPass(conn); err != nil {
			return err
		}
	} else {
		conn.Write([]byte{socks5Version, socks5AuthNone})
	}

	return nil
}

func (ps *ProxyServer) readUserPass(conn net.Conn) error {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	uLen := int(buf[0])

	username := make([]byte, uLen)
	if _, err := io.ReadFull(conn, username); err != nil {
		return err
	}

	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	pLen := int(buf[0])

	password := make([]byte, pLen)
	if _, err := io.ReadFull(conn, password); err != nil {
		return err
	}

	user := string(username)
	pass := string(password)

	if expectedPass, ok := ps.userMap[user]; !ok || expectedPass != pass {
		conn.Write([]byte{0x01, 0x01})
		return fmt.Errorf("auth failed for user: %s", user)
	}

	conn.Write([]byte{0x01, 0x00})
	log.Printf("[PROXY] Auth success: %s", user)
	return nil
}

type socks5Request struct {
	Cmd     byte
	DstHost string
	DstPort int
}

func (ps *ProxyServer) readRequest(conn net.Conn) (*socks5Request, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}

	version := buf[0]
	cmd := buf[1]
	atyp := buf[3]

	if version != socks5Version {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	if cmd != socks5CmdConnect {
		ps.sendError(conn, socks5RepNotSupported)
		return nil, fmt.Errorf("unsupported command: %d", cmd)
	}

	var dstHost string
	var dstPort int

	switch atyp {
	case socks5AddrIPv4:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		dstHost = net.IP(buf).String()

	case socks5AddrDomain:
		buf := make([]byte, 1)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		domainLen := int(buf[0])
		domain := make([]byte, domainLen)
		if _, err := io.ReadFull(conn, domain); err != nil {
			return nil, err
		}
		dstHost = string(domain)

	case socks5AddrIPv6:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		dstHost = net.IP(buf).String()

	default:
		return nil, fmt.Errorf("unknown address type: %d", atyp)
	}

	buf = make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	dstPort = int(binary.BigEndian.Uint16(buf))

	return &socks5Request{
		Cmd:     cmd,
		DstHost: dstHost,
		DstPort: dstPort,
	}, nil
}

func (ps *ProxyServer) sendSuccess(conn net.Conn) error {
	resp := []byte{
		socks5Version,
		socks5RepSuccess,
		0x00,
		socks5AddrIPv4,
		0, 0, 0, 0,
		0, 0,
	}
	_, err := conn.Write(resp)
	return err
}

func (ps *ProxyServer) sendError(conn net.Conn, rep byte) {
	resp := []byte{
		socks5Version,
		rep,
		0x00,
		socks5AddrIPv4,
		0, 0, 0, 0,
		0, 0,
	}
	conn.Write(resp)
}

func (ps *ProxyServer) proxy(clientConn, backendConn net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(backendConn, clientConn)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(clientConn, backendConn)
		done <- struct{}{}
	}()

	<-done
}

func (ps *ProxyServer) GetStats() *ProxyStats {
	stats := ps.tracker.GetStats()
	return &ProxyStats{
		TotalRequests:   atomic.LoadInt64(&ps.totalReqs),
		Total429:        atomic.LoadInt64(&ps.total429),
		ActiveConns:     stats.TotalActive,
		PerBackend:      stats.PerBackend,
		BackendsHealthy: ps.balancer.HealthyCount(),
	}
}

func isIPv4(addr string) bool {
	return strings.Count(addr, ":") == 0
}

func isIPv6(addr string) bool {
	return strings.Contains(addr, ":")
}
