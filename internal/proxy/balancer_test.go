package proxy

import (
	"sync"
	"testing"
)

func TestLoadBalancer_AddBackend(t *testing.T) {
	tests := []struct {
		name      string
		backends  []struct{ id, name, addr string; port int }
		wantCount int
	}{
		{
			name: "add one backend",
			backends: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
			},
			wantCount: 1,
		},
		{
			name: "add multiple backends",
			backends: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
				{"id2", "backend2", "10.0.0.2:1080", 1080},
				{"id3", "backend3", "10.0.0.3:1080", 1080},
			},
			wantCount: 3,
		},
		{
			name: "duplicate id updates",
			backends: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
				{"id1", "backend1-v2", "10.0.0.1:1081", 1081},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer("roundrobin")

			for _, b := range tt.backends {
				lb.AddBackend(b.id, b.name, b.addr, b.port)
			}

			if len(lb.GetBackends()) != tt.wantCount {
				t.Errorf("AddBackend() count = %d, want %d", len(lb.GetBackends()), tt.wantCount)
			}
		})
	}
}

func TestLoadBalancer_Next(t *testing.T) {
	tests := []struct {
		name      string
		backends  []struct{ id, name, addr string; port int }
		calls     int
		wantOrder []string
	}{
		{
			name: "round robin single",
			backends: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
			},
			calls:     3,
			wantOrder: []string{"id1", "id1", "id1"},
		},
		{
			name: "round robin multiple",
			backends: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
				{"id2", "backend2", "10.0.0.2:1080", 1080},
				{"id3", "backend3", "10.0.0.3:1080", 1080},
			},
			calls: 6,
			wantOrder: []string{"id2", "id3", "id1", "id2", "id3", "id1"}, // starts from index 0, round-robin cycles
		},
		{
			name:      "empty backends",
			backends:  []struct{ id, name, addr string; port int }{},
			calls:     3,
			wantOrder: []string{"", "", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer("roundrobin")

			for _, b := range tt.backends {
				lb.AddBackend(b.id, b.name, b.addr, b.port)
			}

			for i := 0; i < tt.calls; i++ {
				got := lb.Next()
				wantID := ""
				if i < len(tt.wantOrder) {
					wantID = tt.wantOrder[i]
				}

				if got == nil && wantID != "" {
					t.Errorf("Next() call %d = nil, want %s", i, wantID)
				} else if got != nil && got.ID != wantID {
					t.Errorf("Next() call %d = %s, want %s", i, got.ID, wantID)
				}
			}
		})
	}
}

func TestLoadBalancer_NextSkipTried(t *testing.T) {
	tests := []struct {
		name   string
		tried  map[string]bool
		wantID string
	}{
		{
			name:   "skip first",
			tried:  map[string]bool{"id1": true},
			wantID: "id2",
		},
		{
			name:   "skip all tried",
			tried:  map[string]bool{"id1": true, "id2": true, "id3": true},
			wantID: "",
		},
		{
			name:   "none tried",
			tried:  map[string]bool{},
			wantID: "id1",
		},
	}

	lb := NewLoadBalancer("roundrobin")
	lb.AddBackend("id1", "backend1", "10.0.0.1:1080", 1080)
	lb.AddBackend("id2", "backend2", "10.0.0.2:1080", 1080)
	lb.AddBackend("id3", "backend3", "10.0.0.3:1080", 1080)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lb.NextSkipTried(tt.tried)
			if tt.wantID == "" && got != nil {
				t.Errorf("NextSkipTried() = %v, want nil", got)
			} else if tt.wantID != "" && (got == nil || got.ID != tt.wantID) {
				t.Errorf("NextSkipTried() = %v, want %s", got, tt.wantID)
			}
		})
	}
}

func TestLoadBalancer_IncrementDecrementConnections(t *testing.T) {
	lb := NewLoadBalancer("roundrobin")
	lb.AddBackend("id1", "backend1", "10.0.0.1:1080", 1080)

	lb.IncrementConnections("id1")
	lb.IncrementConnections("id1")
	lb.IncrementConnections("id1")

	backends := lb.GetBackends()
	if backends[0].Connections != 3 {
		t.Errorf("Connections = %d, want 3", backends[0].Connections)
	}

	lb.DecrementConnections("id1")

	backends = lb.GetBackends()
	if backends[0].Connections != 2 {
		t.Errorf("Connections = %d, want 2", backends[0].Connections)
	}
}

func TestLoadBalancer_SyncBackends(t *testing.T) {
	tests := []struct {
		name      string
		initial   []struct{ id, name, addr string; port int }
		sync      []struct{ id, name, addr string; port int }
		wantCount int
	}{
		{
			name: "add new",
			initial: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
			},
			sync: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
				{"id2", "backend2", "10.0.0.2:1080", 1080},
			},
			wantCount: 2,
		},
		{
			name: "remove old",
			initial: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
				{"id2", "backend2", "10.0.0.2:1080", 1080},
			},
			sync: []struct{ id, name, addr string; port int }{
				{"id1", "backend1", "10.0.0.1:1080", 1080},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer("roundrobin")

			for _, b := range tt.initial {
				lb.AddBackend(b.id, b.name, b.addr, b.port)
			}

			syncBackends := make([]struct{ ID, Name, Address string; Port int }, len(tt.sync))
			for i, b := range tt.sync {
				syncBackends[i] = struct{ ID, Name, Address string; Port int }{b.id, b.name, b.addr, b.port}
			}
			lb.SyncBackends(syncBackends)

			if len(lb.GetBackends()) != tt.wantCount {
				t.Errorf("SyncBackends() count = %d, want %d", len(lb.GetBackends()), tt.wantCount)
			}
		})
	}
}

func TestLoadBalancer_Concurrent(t *testing.T) {
	lb := NewLoadBalancer("roundrobin")
	lb.AddBackend("id1", "backend1", "10.0.0.1:1080", 1080)
	lb.AddBackend("id2", "backend2", "10.0.0.2:1080", 1080)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := lb.Next()
			if b != nil {
				lb.IncrementConnections(b.ID)
				lb.DecrementConnections(b.ID)
			}
		}()
	}
	wg.Wait()
}

func TestNewLoadBalancer(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		wantNil   bool
	}{
		{"roundrobin", "roundrobin", false},
		{"leastconn", "leastconn", false},
		{"iphash", "iphash", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewLoadBalancer(tt.algorithm)
			if (lb == nil) != tt.wantNil {
				t.Errorf("NewLoadBalancer(%q) = %v, wantNil %v", tt.algorithm, lb, tt.wantNil)
			}
		})
	}
}
