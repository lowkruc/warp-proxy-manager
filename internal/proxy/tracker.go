package proxy

import (
	"sync"
	"time"
)

type ConnectionTracker struct {
	mu       sync.RWMutex
	connections map[string]*Connection  // conn_id -> connection
	byBackend   map[string]int          // backend_id -> count
}

type Connection struct {
	ID          string
	BackendID   string
	SrcIP       string
	DstHost     string
	DstPort     int
	StartedAt   time.Time
	BytesSent   int64
	BytesRecv   int64
}

type ConnectionStats struct {
	TotalActive   int
	PerBackend    map[string]int
	AvgPerBackend float64
}

func NewConnectionTracker() *ConnectionTracker {
	return &ConnectionTracker{
		connections: make(map[string]*Connection),
		byBackend:   make(map[string]int),
	}
}

func (ct *ConnectionTracker) Track(connID, backendID, srcIP, dstHost string, dstPort int) *Connection {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	conn := &Connection{
		ID:        connID,
		BackendID: backendID,
		SrcIP:     srcIP,
		DstHost:   dstHost,
		DstPort:   dstPort,
		StartedAt: time.Now(),
	}

	ct.connections[connID] = conn
	ct.byBackend[backendID]++

	return conn
}

func (ct *ConnectionTracker) Untrack(connID string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	conn, ok := ct.connections[connID]
	if !ok {
		return
	}

	if ct.byBackend[conn.BackendID] > 0 {
		ct.byBackend[conn.BackendID]--
	}
	delete(ct.connections, connID)
}

func (ct *ConnectionTracker) GetStats() *ConnectionStats {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	stats := &ConnectionStats{
		TotalActive: len(ct.connections),
		PerBackend:  make(map[string]int),
	}

	for backendID, count := range ct.byBackend {
		if count > 0 {
			stats.PerBackend[backendID] = count
		}
	}

	if len(stats.PerBackend) > 0 {
		total := 0
		for _, count := range stats.PerBackend {
			total += count
		}
		stats.AvgPerBackend = float64(total) / float64(len(stats.PerBackend))
	}

	return stats
}

func (ct *ConnectionTracker) GetConnection(connID string) *Connection {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return ct.connections[connID]
}

func (ct *ConnectionTracker) GetConnectionsByBackend(backendID string) []*Connection {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	var result []*Connection
	for _, conn := range ct.connections {
		if conn.BackendID == backendID {
			result = append(result, conn)
		}
	}
	return result
}

func (ct *ConnectionTracker) UpdateBytes(connID string, sent, recv int64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if conn, ok := ct.connections[connID]; ok {
		conn.BytesSent += sent
		conn.BytesRecv += recv
	}
}
