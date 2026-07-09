package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db     *sql.DB
	mu     sync.RWMutex
}

type MetricRecord struct {
	ID              int64     `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	ActiveConns     int       `json:"active_connections"`
	BackendCount    int       `json:"backend_count"`
	Response429     int       `json:"response_429"`
	Response5xx     int       `json:"response_5xx"`
	AvgLatencyMs    float64   `json:"avg_latency_ms"`
}

type ScaleEventRecord struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	From      int       `json:"from"`
	To        int       `json:"to"`
	Reason    string    `json:"reason"`
	Trigger   string    `json:"trigger"`
}

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			active_connections INTEGER DEFAULT 0,
			backend_count INTEGER DEFAULT 0,
			response_429 INTEGER DEFAULT 0,
			response_5xx INTEGER DEFAULT 0,
			avg_latency_ms REAL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS scale_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			from_count INTEGER,
			to_count INTEGER,
			reason TEXT,
			trigger_name TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_scale_events_timestamp ON scale_events(timestamp)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	return nil
}

func (s *Store) SaveMetric(m *MetricRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO metrics (active_connections, backend_count, response_429, response_5xx, avg_latency_ms) 
		 VALUES (?, ?, ?, ?, ?)`,
		m.ActiveConns, m.BackendCount, m.Response429, m.Response5xx, m.AvgLatencyMs,
	)
	return err
}

func (s *Store) GetMetrics(window string, limit int) ([]*MetricRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var interval string
	switch window {
	case "1m":
		interval = "1 minute"
	case "5m":
		interval = "5 minutes"
	case "15m":
		interval = "15 minutes"
	default:
		interval = "1 minute"
	}

	query := fmt.Sprintf(`
		SELECT id, timestamp, active_connections, backend_count, response_429, response_5xx, avg_latency_ms
		FROM metrics
		WHERE timestamp > datetime('now', '-%s')
		ORDER BY timestamp DESC
		LIMIT ?
	`, interval)

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*MetricRecord
	for rows.Next() {
		m := &MetricRecord{}
		if err := rows.Scan(&m.ID, &m.Timestamp, &m.ActiveConns, &m.BackendCount, &m.Response429, &m.Response5xx, &m.AvgLatencyMs); err != nil {
			return nil, err
		}
		results = append(results, m)
	}

	return results, nil
}

func (s *Store) SaveScaleEvent(e *ScaleEventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO scale_events (from_count, to_count, reason, trigger_name) VALUES (?, ?, ?, ?)`,
		e.From, e.To, e.Reason, e.Trigger,
	)
	return err
}

func (s *Store) GetScaleEvents(limit int) ([]*ScaleEventRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, timestamp, from_count, to_count, reason, trigger_name 
		 FROM scale_events ORDER BY timestamp DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*ScaleEventRecord
	for rows.Next() {
		e := &ScaleEventRecord{}
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.From, &e.To, &e.Reason, &e.Trigger); err != nil {
			return nil, err
		}
		results = append(results, e)
	}

	return results, nil
}

func (s *Store) Cleanup(days int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`DELETE FROM metrics WHERE timestamp < datetime('now', '-%d days')`, days,
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`DELETE FROM scale_events WHERE timestamp < datetime('now', '-%d days')`, days,
	)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}
