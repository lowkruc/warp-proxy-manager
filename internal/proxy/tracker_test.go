package proxy

import (
	"testing"
)

func TestConnectionTracker_TrackUntrack(t *testing.T) {
	tracker := NewConnectionTracker()

	tests := []struct {
		name    string
		connID  string
		backend string
		srcIP   string
		host    string
		port    int
	}{
		{"track connection 1", "conn1", "backend1", "10.0.0.1:5000", "example.com", 443},
		{"track connection 2", "conn2", "backend2", "10.0.0.2:5001", "api.example.com", 8080},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := tracker.Track(tt.connID, tt.backend, tt.srcIP, tt.host, tt.port)

			if conn == nil {
				t.Fatal("Track() returned nil")
			}
			if conn.ID != tt.connID {
				t.Errorf("Track() ID = %q, want %q", conn.ID, tt.connID)
			}
			if conn.BackendID != tt.backend {
				t.Errorf("Track() BackendID = %q, want %q", conn.BackendID, tt.backend)
			}
		})
	}

	// Check stats
	stats := tracker.GetStats()
	if stats.TotalActive != 2 {
		t.Errorf("GetStats() TotalActive = %d, want 2", stats.TotalActive)
	}

	// Untrack
	tracker.Untrack("conn1")
	stats = tracker.GetStats()
	if stats.TotalActive != 1 {
		t.Errorf("After Untrack() TotalActive = %d, want 1", stats.TotalActive)
	}
}

func TestConnectionTracker_GetConnectionsByBackend(t *testing.T) {
	tracker := NewConnectionTracker()

	tracker.Track("conn1", "backend1", "10.0.0.1:5000", "example.com", 443)
	tracker.Track("conn2", "backend1", "10.0.0.1:5001", "api.example.com", 80)
	tracker.Track("conn3", "backend2", "10.0.0.2:5000", "test.com", 443)

	conns := tracker.GetConnectionsByBackend("backend1")
	if len(conns) != 2 {
		t.Errorf("GetConnectionsByBackend(backend1) count = %d, want 2", len(conns))
	}

	conns = tracker.GetConnectionsByBackend("backend2")
	if len(conns) != 1 {
		t.Errorf("GetConnectionsByBackend(backend2) count = %d, want 1", len(conns))
	}

	conns = tracker.GetConnectionsByBackend("nonexistent")
	if len(conns) != 0 {
		t.Errorf("GetConnectionsByBackend(nonexistent) count = %d, want 0", len(conns))
	}
}

func TestConnectionTracker_UpdateBytes(t *testing.T) {
	tracker := NewConnectionTracker()

	tracker.Track("conn1", "backend1", "10.0.0.1:5000", "example.com", 443)

	tracker.UpdateBytes("conn1", 100, 200)
	tracker.UpdateBytes("conn1", 50, 75)

	conn := tracker.GetConnection("conn1")
	if conn.BytesSent != 150 {
		t.Errorf("BytesSent = %d, want 150", conn.BytesSent)
	}
	if conn.BytesRecv != 275 {
		t.Errorf("BytesRecv = %d, want 275", conn.BytesRecv)
	}
}
