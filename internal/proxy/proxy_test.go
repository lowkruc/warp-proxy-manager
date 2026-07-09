package proxy

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestIsIPv4(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"valid ipv4", "192.168.1.1", true},
		{"valid ipv4 loopback", "127.0.0.1", true},
		{"valid ipv4 zeros", "0.0.0.0", true},
		{"domain", "ifconfig.me", false},
		{"domain with sub", "api.example.com", false},
		{"empty", "", false},
		{"ipv6", "::1", false},
		{"ipv6 full", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", false},
		{"mixed", "example.com:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPv4(tt.addr)
			if got != tt.want {
				t.Errorf("isIPv4(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIsIPv6(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"valid ipv6", "::1", true},
		{"valid ipv6 full", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", true},
		{"ipv4", "192.168.1.1", false},
		{"domain", "example.com", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPv6(tt.addr)
			if got != tt.want {
				t.Errorf("isIPv6(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestHandshake(t *testing.T) {
	tests := []struct {
		name      string
		auth      bool
		clientMsg []byte
		wantErr   bool
		wantResp  byte
	}{
		{
			name:      "no auth success",
			auth:      false,
			clientMsg: []byte{0x05, 0x01, 0x00},
			wantErr:   false,
			wantResp:  0x00,
		},
		{
			name:      "unsupported version",
			auth:      false,
			clientMsg: []byte{0x04, 0x01, 0x00},
			wantErr:   true,
		},
		{
			name:      "auth required but client offers none",
			auth:      true,
			clientMsg: []byte{0x05, 0x01, 0x00},
			wantErr:   true,
		},
		{
			name:      "auth required client offers userpass",
			auth:      true,
			clientMsg: []byte{0x05, 0x02, 0x00, 0x02},
			wantErr:   false,
			wantResp:  0x02,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()

			ps := &ProxyServer{
				config: &ProxyConfig{
					AuthEnabled: tt.auth,
				},
			}

			go func() {
				client.Write(tt.clientMsg)
				if !tt.wantErr {
					resp := make([]byte, 2)
					client.Read(resp)
				}
			}()

			err := ps.handshake(server)
			if (err != nil) != tt.wantErr {
				t.Errorf("handshake() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadRequest(t *testing.T) {
	tests := []struct {
		name     string
		req      []byte
		wantCmd  byte
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{
			name: "connect domain",
			req: func() []byte {
				host := "ifconfig.me"
				buf := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
				buf = append(buf, host...)
				port := make([]byte, 2)
				binary.BigEndian.PutUint16(port, 443)
				buf = append(buf, port...)
				return buf
			}(),
			wantCmd:  0x01,
			wantHost: "ifconfig.me",
			wantPort: 443,
		},
		{
			name: "connect ipv4",
			req: func() []byte {
				buf := []byte{0x05, 0x01, 0x00, 0x01, 192, 168, 1, 1}
				port := make([]byte, 2)
				binary.BigEndian.PutUint16(port, 8080)
				buf = append(buf, port...)
				return buf
			}(),
			wantCmd:  0x01,
			wantHost: "192.168.1.1",
			wantPort: 8080,
		},
		{
			name: "connect ipv6",
			req: func() []byte {
				buf := []byte{0x05, 0x01, 0x00, 0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
				port := make([]byte, 2)
				binary.BigEndian.PutUint16(port, 443)
				buf = append(buf, port...)
				return buf
			}(),
			wantCmd:  0x01,
			wantHost: "::1",
			wantPort: 443,
		},
		{
			name:    "unsupported cmd",
			req:     []byte{0x05, 0x02, 0x00, 0x01, 192, 168, 1, 1, 0, 80},
			wantErr: true,
		},
		{
			name:    "unsupported version",
			req:     []byte{0x04, 0x01, 0x00, 0x01, 192, 168, 1, 1, 0, 80},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()

			ps := &ProxyServer{
				config: &ProxyConfig{},
			}

			go func() {
				client.Write(tt.req)
			}()

			req, err := ps.readRequest(server)
			if (err != nil) != tt.wantErr {
				t.Errorf("readRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if req.Cmd != tt.wantCmd {
					t.Errorf("readRequest() Cmd = %x, want %x", req.Cmd, tt.wantCmd)
				}
				if req.DstHost != tt.wantHost {
					t.Errorf("readRequest() DstHost = %q, want %q", req.DstHost, tt.wantHost)
				}
				if req.DstPort != tt.wantPort {
					t.Errorf("readRequest() DstPort = %d, want %d", req.DstPort, tt.wantPort)
				}
			}
		})
	}
}

func TestSendSuccess(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ps := &ProxyServer{}

	go func() {
		ps.sendSuccess(server)
	}()

	resp := make([]byte, 10)
	client.Read(resp)

	if resp[0] != 0x05 {
		t.Errorf("sendSuccess() version = %x, want 0x05", resp[0])
	}
	if resp[1] != 0x00 {
		t.Errorf("sendSuccess() rep = %x, want 0x00", resp[1])
	}
}

func TestSendError(t *testing.T) {
	tests := []struct {
		name string
		rep  byte
	}{
		{"general failure", 0x01},
		{"not allowed", 0x02},
		{"network unreachable", 0x03},
		{"host unreachable", 0x04},
		{"connection refused", 0x05},
		{"not supported", 0x07},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()

			ps := &ProxyServer{}

			go func() {
				ps.sendError(server, tt.rep)
			}()

			resp := make([]byte, 10)
			client.Read(resp)

			if resp[0] != 0x05 {
				t.Errorf("sendError() version = %x, want 0x05", resp[0])
			}
			if resp[1] != tt.rep {
				t.Errorf("sendError() rep = %x, want %x", resp[1], tt.rep)
			}
		})
	}
}

func TestForwardRequest(t *testing.T) {
	tests := []struct {
		name     string
		req      *socks5Request
		wantData []byte
	}{
		{
			name: "domain request",
			req: &socks5Request{
				Cmd:     0x01,
				DstHost: "ifconfig.me",
				DstPort: 443,
			},
			wantData: func() []byte {
				host := "ifconfig.me"
				buf := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
				buf = append(buf, host...)
				port := make([]byte, 2)
				binary.BigEndian.PutUint16(port, 443)
				buf = append(buf, port...)
				return buf
			}(),
		},
		{
			name: "ipv4 request",
			req: &socks5Request{
				Cmd:     0x01,
				DstHost: "10.0.0.1",
				DstPort: 8080,
			},
			wantData: func() []byte {
				buf := []byte{0x05, 0x01, 0x00, 0x01, 10, 0, 0, 1}
				port := make([]byte, 2)
				binary.BigEndian.PutUint16(port, 8080)
				buf = append(buf, port...)
				return buf
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()

			ps := &ProxyServer{
				config: &ProxyConfig{},
			}

			go func() {
				// Read client request and send SOCKS5 success response
				buf := make([]byte, 1024)
				server.Read(buf)
				server.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			}()

			err := ps.forwardRequest(client, server, tt.req)
			if err != nil {
				t.Errorf("forwardRequest() error = %v", err)
				return
			}

			got := make([]byte, len(tt.wantData))
			client.SetReadDeadline(time.Now().Add(time.Second))
			client.Read(got)

			for i := range tt.wantData {
				if got[i] != tt.wantData[i] {
					t.Errorf("forwardRequest() byte[%d] = %x, want %x", i, got[i], tt.wantData[i])
				}
			}
		})
	}
}
