package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/lowkruc/warp-proxy-manager/internal/config"
)

func TestParseMemory(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"kilobytes", "150k", 153600, false},
		{"kilobytes KB", "150KB", 153600, false},
		{"megabytes", "150m", 157286400, false},
		{"megabytes MB", "150MB", 157286400, false},
		{"gigabytes", "2g", 2147483648, false},
		{"gigabytes GB", "2GB", 2147483648, false},
		{"decimal megabytes", "1.5m", 1572864, false},
		{"raw bytes", "1024", 1024, false},
		{"empty", "", 0, false},
		{"zero", "0", 0, false},
		{"case insensitive", "150M", 157286400, false},
		{"whitespace", "  150m  ", 157286400, false},
		{"unknown unit", "150x", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMemory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMemory(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseMemory(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"long", "12345678901234567890", "123456789012"},
		{"exact", "123456789012", "123456789012"},
		{"short", "abc", "abc"},
		{"empty", "", ""},
		{"one", "a", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateID(tt.id)
			if got != tt.want {
				t.Errorf("truncateID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

// testDockerServer creates a mock Docker API server.
func testDockerServer(t *testing.T, handler http.HandlerFunc) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go http.Serve(ln, handler)
	return ln.Addr().String(), func() { ln.Close() }
}

func newTestClient(addr string) *Client {
	return &Client{
		config:     config.DefaultConfig(),
		baseURL:    "http://" + addr,
		httpClient: &http.Client{},
	}
}

func TestListContainers_LabelFilter(t *testing.T) {
	managed := []ContainerListResponse{
		{
			ID:    "aaa111bbb222ccc",
			Names: []string{"/warp-proxy-abc123"},
			State: "running",
			Labels: map[string]string{"warp-proxy-managed": "true"},
		},
	}
	mixed := []ContainerListResponse{
		{
			ID:    "aaa111bbb222ccc",
			Names: []string{"/warp-proxy-abc123"},
			State: "running",
			Labels: map[string]string{"warp-proxy-managed": "true"},
		},
		{
			ID:    "ddd444eee555fff",
			Names: []string{"/warp-proxy-manager-manager-1"},
			State: "running",
			Labels: map[string]string{},
		},
		{
			ID:    "ggg777hhh888iii",
			Names: []string{"/my-app"},
			State: "running",
			Labels: map[string]string{"com.docker.compose.project": "myapp"},
		},
	}

	tests := []struct {
		name     string
		data     []ContainerListResponse
		wantLen  int
		wantName string
	}{
		{"only managed", managed, 1, "warp-proxy-abc123"},
		{"mixed filters out unmanaged", mixed, 1, "warp-proxy-abc123"},
		{"empty", []ContainerListResponse{}, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(tt.data)
			addr, cleanup := testDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, "%s", data)
			})
			defer cleanup()

			c := newTestClient(addr)
			containers, err := c.ListContainers(context.Background())
			if err != nil {
				t.Fatalf("ListContainers() error = %v", err)
			}
			if len(containers) != tt.wantLen {
				t.Errorf("ListContainers() count = %d, want %d", len(containers), tt.wantLen)
			}
			if tt.wantLen > 0 && containers[0].Name != tt.wantName {
				t.Errorf("ListContainers()[0].Name = %q, want %q", containers[0].Name, tt.wantName)
			}
		})
	}
}

func TestCleanupManaged(t *testing.T) {
	deleteCalled := 0
	containers := []ContainerListResponse{
		{
			ID:    "aaa111bbb222ccc",
			Names: []string{"/warp-proxy-abc123"},
			State: "running",
			Labels: map[string]string{"warp-proxy-managed": "true"},
		},
		{
			ID:    "ddd444eee555fff",
			Names: []string{"/warp-proxy-def456"},
			State: "running",
			Labels: map[string]string{"warp-proxy-managed": "true"},
		},
	}
	data, _ := json.Marshal(containers)

	addr, cleanup := testDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "%s", data)
		} else if r.Method == "DELETE" {
			deleteCalled++
			w.WriteHeader(http.StatusNoContent)
		}
	})
	defer cleanup()

	c := newTestClient(addr)
	removed := c.CleanupManaged(context.Background())

	if removed != 2 {
		t.Errorf("CleanupManaged() removed = %d, want 2", removed)
	}
	if deleteCalled != 2 {
		t.Errorf("DELETE calls = %d, want 2", deleteCalled)
	}
}
