package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lowkruc/warp-proxy-manager/internal/config"
)

const (
	socks5Version = 0x05
	socks5AuthNone = 0x00
	socks5AuthUserPass = 0x02
	socks5AuthFailed = 0xFF
	socks5CmdConnect = 0x01
	socks5AddrIPv4 = 0x01
	socks5AddrDomain = 0x03
	socks5AddrIPv6 = 0x04
	socks5RepSuccess = 0x00
	socks5RepFailure = 0x01
	socks5RepNotSupported = 0x07
)

type ProxyServer struct {
	config      *config.Config
	balancer    *LoadBalancer
	tracker     *ConnectionTracker
	listener    net.Listener
	wg          sync.WaitGroup
	running     bool
	userMap     map[string]string // user -> bcrypt_hash
}

type ProxyRequest struct {
	ID        string
	SrcAddr   net.Addr
	DstHost   string
	DstPort   int
	Backend   *Backend
	StartTime time.Time
}

func NewProxyServer(cfg *config.Config, balancer *LoadBalancer) *ProxyServer {
	return &ProxyServer{
		config:   cfg,
		balancer: balancer,
		tracker:  NewConnectionTracker(),
		userMap:  make(map[string]string),
	}
}

func (ps *ProxyServer) Start() error {
	// Build user map
	for _, u := range ps.config.Proxy.Auth.Users {
		ps.userMap[u.User] = u.Pass
	}

	var err error
	ps.listener, err = net.Listen("tcp", ps.config.Proxy.Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	ps.running = true
	log.Printf("[PROXY] Listening on %s", ps.config.Proxy.Listen)

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
	for {
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

func (ps *ProxyServer) handleConn(conn net.Conn) {
	defer ps.wg.Done()
	defer conn.Close()

	// SOCKS5 handshake
	if err := ps.handshake(conn); err != nil {
		log.Printf("[PROXY] Handshake failed: %v", err)
		return
	}

	// Read request
	req, err := ps.readRequest(conn)
	if err != nil {
		log.Printf("[PROXY] Read request failed: %v", err)
		return
	}

	log.Printf("[PROXY] Request: %s:%d", req.DstHost, req.DstPort)

	// Find backend
	backend := ps.balancer.Next()
	if backend == nil {
		ps.sendError(conn, socks5RepFailure)
		return
	}

	log.Printf("[PROXY] Using backend: %s (%s)", backend.Name, backend.Address)

	// Connect to backend
	backendConn, err := net.DialTimeout("tcp", backend.Address, ps.config.Proxy.Timeout.Connect)
	if err != nil {
		log.Printf("[PROXY] Connect to backend failed: %v", err)
		ps.sendError(conn, socks5RepFailure)
		return
	}
	defer backendConn.Close()

	// Send success response
	if err := ps.sendSuccess(conn); err != nil {
		log.Printf("[PROXY] Send success failed: %v", err)
		return
	}

	// Track connection
	connID := fmt.Sprintf("%s-%d", conn.RemoteAddr(), time.Now().UnixNano())
	ps.tracker.Track(connID, backend.ID, conn.RemoteAddr().String(), req.DstHost, req.DstPort)
	ps.balancer.IncrementConnections(backend.ID)

	defer func() {
		ps.tracker.Untrack(connID)
		ps.balancer.DecrementConnections(backend.ID)
	}()

	// Bidirectional copy
	ps.proxy(conn, backendConn)
}

func (ps *ProxyServer) handshake(conn net.Conn) error {
	// Read version + auth methods
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

	// Check auth
	if ps.config.Proxy.Auth.Enabled {
		// Check if username/password auth is supported
		supported := false
		for _, m := range methods {
			if m == socks5AuthUserPass {
				supported = true
				break
			}
		}

		if !supported {
			// No auth supported by client
			conn.Write([]byte{socks5Version, socks5AuthFailed})
			return fmt.Errorf("client doesn't support user/pass auth")
		}

		// Select user/pass auth
		conn.Write([]byte{socks5Version, socks5AuthUserPass})

		// Read username/password
		if err := ps.readUserPass(conn); err != nil {
			return err
		}
	} else {
		// No auth required
		conn.Write([]byte{socks5Version, socks5AuthNone})
	}

	return nil
}

func (ps *ProxyServer) readUserPass(conn net.Conn) error {
	// Read uLEN + username + pLEN + password
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

	// Verify
	user := string(username)
	pass := string(password)

	if expectedPass, ok := ps.userMap[user]; !ok || expectedPass != pass {
		conn.Write([]byte{0x01, 0x01}) // Auth failure
		return fmt.Errorf("auth failed for user: %s", user)
	}

	conn.Write([]byte{0x01, 0x00}) // Auth success
	log.Printf("[PROXY] Auth success: %s", user)
	return nil
}

type socks5Request struct {
	Cmd     byte
	DstHost string
	DstPort int
}

func (ps *ProxyServer) readRequest(conn net.Conn) (*socks5Request, error) {
	// Read: VER CMD RSV ATYP DST.ADDR DST.PORT
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}

	version := buf[0]
	cmd := buf[1]
	// rsv := buf[2]
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

	// Read port
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
	// VER REP RSV ATYP BND.ADDR BND.PORT
	resp := []byte{
		socks5Version,
		socks5RepSuccess,
		0x00, // RSV
		socks5AddrIPv4,
		0, 0, 0, 0, // BND.ADDR (0.0.0.0)
		0, 0, // BND.PORT (0)
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

func (ps *ProxyServer) GetStats() map[string]interface{} {
	stats := ps.tracker.GetStats()
	return map[string]interface{}{
		"active_connections": stats.TotalActive,
		"per_backend":        stats.PerBackend,
		"avg_per_backend":    stats.AvgPerBackend,
	}
}

func (ps *ProxyServer) ParseAddress(addr string) (string, int) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func isIPv6(addr string) bool {
	return strings.Contains(addr, ":")
}
